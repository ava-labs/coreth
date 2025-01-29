// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/libevm/log"
	ethp2p "github.com/ava-labs/libevm/p2p"
	"github.com/ava-labs/libevm/p2p/enode"
)

var (
	_ p2p.Handler          = (*Handler)(nil)
	_ snap.Backend         = (*Handler)(nil)
	_ ethp2p.MsgReadWriter = (*rw)(nil)
)

const (
	ProtocolID               = 128 // ID for the state sync handler, leaving earlier IDs reserved
	ErrCodeSnapHandlerFailed = 1

	protocolVersion = 0
)

type Handler struct {
	chain *core.BlockChain
}

func NewHandler(chain *core.BlockChain) *Handler {
	return &Handler{chain: chain}
}

func (h *Handler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	deadline time.Time,
	requestBytes []byte,
) ([]byte, *common.AppError) {
	start := time.Now()
	log.Debug("statesync AppRequest called", "nodeID", nodeID, "requestBytes", len(requestBytes))
	rw := &rw{readBytes: requestBytes}
	p := snap.NewFakePeer(protocolVersion, nodeID.String(), rw)
	err := snap.HandleMessage(h, p)
	log.Debug("statesync AppRequest handled", "nodeID", nodeID, "err", err)
	if err != nil {
		return nil, &common.AppError{
			Code:    ErrCodeSnapHandlerFailed,
			Message: err.Error(),
		}
	}
	log.Debug("statesync AppRequest response", "nodeID", nodeID, "responseBytes", len(rw.writeBytes), "duration", time.Since(start))
	return rw.writeBytes, nil
}

// AppGossip implements p2p.Handler.
// It is implemented as a no-op as gossip is not used in the state sync protocol.
func (h *Handler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {}

// Chain implements snap.Backend.
func (h *Handler) Chain() *core.BlockChain { return h.chain }

// Handle implements snap.Backend.
// It is implemented as a no-op as the main handling is done in AppRequest.
func (h *Handler) Handle(*snap.Peer, snap.Packet) error { return nil }

// RunPeer implements snap.Handler.
// It is not expected to be called in the used code path.
func (h *Handler) RunPeer(*snap.Peer, snap.Handler) error { panic("calling not expected") }

// PeerInfo implements snap.Handler.
// It is not expected to be called in the used code path.
func (h *Handler) PeerInfo(id enode.ID) interface{} { panic("calling not expected") }

// rw is a helper struct that implements ethp2p.MsgReadWriter.
type rw struct {
	readBytes  []byte
	writeBytes []byte
}

// ReadMsg implements ethp2p.MsgReadWriter.
// It is expected to be called exactly once, immediately after the request is received.
func (rw *rw) ReadMsg() (ethp2p.Msg, error) {
	return fromBytes(rw.readBytes)
}

// WriteMsg implements ethp2p.MsgReadWriter.
// It is expected to be called exactly once, immediately after the response is prepared.
func (rw *rw) WriteMsg(msg ethp2p.Msg) error {
	var err error
	rw.writeBytes, err = toBytes(msg)
	return err
}

func fromBytes(msgBytes []byte) (ethp2p.Msg, error) {
	if len(msgBytes) < wrappers.LongLen {
		return ethp2p.Msg{}, fmt.Errorf("bytes too short: %d", len(msgBytes))
	}
	code := binary.BigEndian.Uint64(msgBytes)
	return ethp2p.Msg{
		Code:       code,
		Size:       uint32(len(msgBytes) - wrappers.LongLen),
		Payload:    bytes.NewReader(msgBytes[wrappers.LongLen:]),
		ReceivedAt: time.Now(),
	}, nil
}

func toBytes(msg ethp2p.Msg) ([]byte, error) {
	bytes := make([]byte, msg.Size+wrappers.LongLen)
	binary.BigEndian.PutUint64(bytes, msg.Code)
	n, err := msg.Payload.Read(bytes[wrappers.LongLen:])
	if n == int(msg.Size) && errors.Is(err, io.EOF) {
		err = nil
	}
	return bytes, err
}
