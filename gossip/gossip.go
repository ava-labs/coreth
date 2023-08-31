// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/golang/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	"github.com/ava-labs/coreth/gossip/proto/pb"
)

// GossipableAny exists to help create non-nil pointers to a concrete Gossipable
// ref: https://stackoverflow.com/questions/69573113/how-can-i-instantiate-a-non-nil-pointer-of-type-argument-with-generic-go
type GossipableAny[T any] interface {
	*T
	Gossipable
}

type Config struct {
	Frequency time.Duration
	PollSize  int
}

func NewGossiper[T any, U GossipableAny[T]](
	config Config,
	set Set[U],
	client *p2p.Client,
) *Gossiper[T, U] {
	return &Gossiper[T, U]{
		config: config,
		set:    set,
		client: client,
	}
}

type Gossiper[T any, U GossipableAny[T]] struct {
	config Config
	set    Set[U]
	client *p2p.Client
}

func (g *Gossiper[_, _]) Gossip(ctx context.Context) {
	gossipTicker := time.NewTicker(g.config.Frequency)
	defer gossipTicker.Stop()

	for {
		select {
		case <-gossipTicker.C:
			if err := g.gossip(ctx); err != nil {
				log.Warn("failed to gossip", "error", err)
			}
		case <-ctx.Done():
			log.Debug("shutting down gossip")
			return
		}
	}
}

func (g *Gossiper[_, _]) gossip(ctx context.Context) error {
	filter := g.set.GetFilter()
	bloomBytes, err := filter.Bloom.MarshalBinary()
	if err != nil {
		return err
	}

	request := &pb.PullGossipRequest{
		Filter: bloomBytes,
		Salt:   filter.Salt[:],
	}
	msgBytes, err := proto.Marshal(request)
	if err != nil {
		return err
	}

	for i := 0; i < g.config.PollSize; i++ {
		if err := g.client.AppRequestAny(ctx, msgBytes, g.handleResponse); err != nil {
			return err
		}
	}

	return nil
}

func (g *Gossiper[T, U]) handleResponse(nodeID ids.NodeID, responseBytes []byte, err error) {
	if err != nil {
		log.Debug("failed gossip request", "nodeID", nodeID, "error", err)
		return
	}

	response := &pb.PullGossipResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		log.Debug("failed to unmarshal gossip response", "error", err)
		return
	}

	for _, bytes := range response.Gossip {
		gossipable := U(new(T))
		if err := gossipable.Unmarshal(bytes); err != nil {
			log.Debug("failed to unmarshal gossip", "error", err, "nodeID", nodeID)
			continue
		}

		log.Debug("received gossip", "nodeID", nodeID, "hash", gossipable.GetHash())
		if err := g.set.Add(gossipable); err != nil {
			log.Debug("failed to add gossip to the known set", "error", err, "nodeID", nodeID, "id", gossipable.GetHash())
			continue
		}
	}
}
