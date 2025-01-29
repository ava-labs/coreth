// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/libevm/log"
	ethp2p "github.com/ava-labs/libevm/p2p"
	"github.com/ava-labs/libevm/p2p/enode"
)

var (
	_ validators.Connector = (*Connector)(nil)
	_ ethp2p.MsgReadWriter = (*outbound)(nil)
)

type Connector struct {
	sync   *snap.Syncer
	sender *p2p.Client
}

func NewConnector(sync *snap.Syncer, sender *p2p.Client) *Connector {
	return &Connector{sync: sync, sender: sender}
}

func (c *Connector) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	return c.sync.Register(NewOutboundPeer(nodeID, c.sync, c.sender))
}

func (c *Connector) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return c.sync.Unregister(nodeID.String())
}

type outbound struct {
	peerID ids.NodeID
	sync   *snap.Syncer
	sender *p2p.Client
}

func NewOutboundPeer(nodeID ids.NodeID, sync *snap.Syncer, sender *p2p.Client) *snap.Peer {
	return snap.NewFakePeer(protocolVersion, nodeID.String(), &outbound{
		peerID: nodeID,
		sync:   sync,
		sender: sender,
	})
}

func (o *outbound) WriteMsg(msg ethp2p.Msg) error {
	bytes, err := toBytes(msg)
	if err != nil {
		return fmt.Errorf("failed to convert message to bytes: %w, expected: %d", err, msg.Size)
	}

	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(o.peerID)

	return o.sender.AppRequest(context.Background(), nodeIDs, bytes, o.handleResponse)
}

// ReadMsg implements the ethp2p.MsgReadWriter interface.
// It is not expected to be called in the used code path.
func (o *outbound) ReadMsg() (ethp2p.Msg, error) { panic("not expected to be called") }

func (o *outbound) handleResponse(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	if err != nil {
		log.Warn("got error response from peer", "peer", nodeID, "err", err)
		return
	}

	log.Debug("statesync AppRequest response", "nodeID", nodeID, "responseBytes", len(responseBytes))
	p := snap.NewFakePeer(protocolVersion, nodeID.String(), &rw{responseBytes: responseBytes})
	if err := snap.HandleMessage(o, p); err != nil {
		log.Warn("failed to handle response", "peer", nodeID, "err", err)
	}
}

func (o *outbound) Chain() *core.BlockChain                { panic("not expected to be called") }
func (o *outbound) RunPeer(*snap.Peer, snap.Handler) error { panic("not expected to be called") }
func (o *outbound) PeerInfo(id enode.ID) interface{}       { panic("not expected to be called") }

func (o *outbound) Handle(peer *snap.Peer, packet snap.Packet) error {
	d := &Downloader{SnapSyncer: o.sync}
	return d.DeliverSnapPacket(peer, packet)
}

// Downloader is copied from eth/downloader/downloader.go
type Downloader struct {
	SnapSyncer *snap.Syncer
}

// DeliverSnapPacket is invoked from a peer's message handler when it transmits a
// data packet for the local node to consume.
func (d *Downloader) DeliverSnapPacket(peer *snap.Peer, packet snap.Packet) error {
	switch packet := packet.(type) {
	case *snap.AccountRangePacket:
		hashes, accounts, err := packet.Unpack()
		if err != nil {
			return err
		}
		return d.SnapSyncer.OnAccounts(peer, packet.ID, hashes, accounts, packet.Proof)

	case *snap.StorageRangesPacket:
		hashset, slotset := packet.Unpack()
		return d.SnapSyncer.OnStorage(peer, packet.ID, hashset, slotset, packet.Proof)

	case *snap.ByteCodesPacket:
		return d.SnapSyncer.OnByteCodes(peer, packet.ID, packet.Codes)

	case *snap.TrieNodesPacket:
		return d.SnapSyncer.OnTrieNodes(peer, packet.ID, packet.Nodes)

	default:
		return fmt.Errorf("unexpected snap packet type: %T", packet)
	}
}
