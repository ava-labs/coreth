// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/libevm/log"
	ethp2p "github.com/ava-labs/libevm/p2p"
	"github.com/ava-labs/libevm/p2p/enode"
)

const (
	maxRetries                 = 5
	failedRequestSleepInterval = 10 * time.Millisecond
)

var (
	_ validators.Connector = (*Connector)(nil)
	_ ethp2p.MsgReadWriter = (*outbound)(nil)
)

type Connector struct {
	downloader *Downloader
	sender     *p2p.Client
}

func NewConnector(downloader *Downloader, sender *p2p.Client) *Connector {
	return &Connector{downloader: downloader, sender: sender}
}

func (c *Connector) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	return c.downloader.SnapSyncer.Register(NewOutboundPeer(nodeID, c.downloader, c.sender))
}

func (c *Connector) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return c.downloader.SnapSyncer.Unregister(nodeID.String())
}

type outbound struct {
	peerID     ids.NodeID
	downloader *Downloader
	sender     *p2p.Client
}

func NewOutboundPeer(nodeID ids.NodeID, downloader *Downloader, sender *p2p.Client) *snap.Peer {
	return snap.NewFakePeer(protocolVersion, nodeID.String(), &outbound{
		peerID:     nodeID,
		downloader: downloader,
		sender:     sender,
	})
}

// ReadMsg implements the ethp2p.MsgReadWriter interface.
// It is not expected to be called in the used code path.
func (o *outbound) ReadMsg() (ethp2p.Msg, error) { panic("not expected to be called") }

func (o *outbound) WriteMsg(msg ethp2p.Msg) error {
	bytes, err := toBytes(msg)
	if err != nil {
		return fmt.Errorf("failed to convert message to bytes: %w, expected: %d", err, msg.Size)
	}

	message := &retryableMessage{outbound: o, outBytes: bytes}
	return message.send()
}

type retryableMessage struct {
	outbound *outbound
	outBytes []byte
	retries  int
}

func (r *retryableMessage) send() error {
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(r.outbound.peerID)

	return r.outbound.sender.AppRequest(context.Background(), nodeIDs, r.outBytes, r.handleResponse)
}

func (r *retryableMessage) handleResponse(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	if err == nil { // Handle successful response
		log.Debug("statesync AppRequest response", "nodeID", nodeID, "responseBytes", len(responseBytes))
		p := snap.NewFakePeer(protocolVersion, nodeID.String(), &rw{readBytes: responseBytes})
		if err := snap.HandleMessage(r.outbound, p); err != nil {
			log.Warn("failed to handle response", "peer", nodeID, "err", err)
		}
		return
	}

	// Handle retry

	log.Warn("got error response from peer", "peer", nodeID, "err", err)
	// TODO: Is this the right way to check for AppError?
	// Notably errors.As expects a ptr to a type that implements error interface,
	// but *AppError implements error, not AppError.
	appErr, ok := err.(*common.AppError)
	if !ok {
		log.Warn("unexpected error type", "err", err)
		return
	}
	if appErr.Code != common.ErrTimeout.Code {
		log.Debug("dropping non-timeout error", "peer", nodeID, "err", err)
		return // only retry on timeout
	}
	if r.retries >= maxRetries {
		log.Warn("reached max retries", "peer", nodeID)
		return
	}
	r.retries++
	log.Debug("retrying request", "peer", nodeID, "retries", r.retries)
	time.Sleep(failedRequestSleepInterval)
	if err := r.send(); err != nil {
		log.Warn("failed to retry request, dropping", "peer", nodeID, "err", err)
	}
}

func (o *outbound) Chain() *core.BlockChain                { panic("not expected to be called") }
func (o *outbound) RunPeer(*snap.Peer, snap.Handler) error { panic("not expected to be called") }
func (o *outbound) PeerInfo(id enode.ID) interface{}       { panic("not expected to be called") }

func (o *outbound) Handle(peer *snap.Peer, packet snap.Packet) error {
	return o.downloader.DeliverSnapPacket(peer, packet)
}
