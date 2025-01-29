// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	ethp2p "github.com/ava-labs/libevm/p2p"
)

var (
	_ validators.Connector = (*Connector)(nil)
	_ ethp2p.MsgReadWriter = (*outbound)(nil)
)

type Connector struct {
	sync   *snap.Syncer
	sender common.AppSender
}

func NewConnector(sync *snap.Syncer, sender common.AppSender) *Connector {
	return &Connector{sync: sync, sender: sender}
}

func (c *Connector) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	return c.sync.Register(NewOutboundPeer(nodeID, c.sender))
}

func (c *Connector) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return c.sync.Unregister(nodeID.String())
}

type outbound struct {
	peerID    ids.NodeID
	sender    common.AppSender
	requestID uint32
}

func NewOutboundPeer(nodeID ids.NodeID, sender common.AppSender) *snap.Peer {
	return snap.NewFakePeer(protocolVersion, nodeID.String(), &outbound{
		peerID: nodeID,
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

	o.requestID++
	return o.sender.SendAppRequest(context.Background(), nodeIDs, o.requestID, bytes)
}

// ReadMsg implements the ethp2p.MsgReadWriter interface.
// It is not expected to be called in the used code path.
func (o *outbound) ReadMsg() (ethp2p.Msg, error) { panic("not expected to be called") }
