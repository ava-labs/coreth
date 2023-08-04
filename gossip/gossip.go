// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
)

// GossipableAny exists to help create non-nil pointers to a concrete Gossipable
type GossipableAny[T any] interface {
	*T
	Gossipable
}

func NewGossiper[T any, U GossipableAny[T]](
	set Set[U],
	client *p2p.Client,
	codec codec.Manager,
	codecVersion uint16,
	gossipSize int,
	frequency time.Duration,
) *Gossiper[T, U] {
	return &Gossiper[T, U]{
		set:          set,
		client:       client,
		codec:        codec,
		codecVersion: codecVersion,
		gossipSize:   gossipSize,
		frequency:    frequency,
	}
}

type Gossiper[T any, U GossipableAny[T]] struct {
	set          Set[U]
	client       *p2p.Client
	codec        codec.Manager
	codecVersion uint16
	gossipSize   int
	frequency    time.Duration
}

func (g *Gossiper[T, U]) Gossip(shutdownChan chan struct{}, shutdownWg *sync.WaitGroup) {
	gossipTicker := time.NewTicker(g.frequency)
	defer func() {
		gossipTicker.Stop()
		shutdownWg.Done()
	}()

	for {
		select {
		case <-gossipTicker.C:
			filter := g.set.GetFilter()
			filterBytes, err := g.codec.Marshal(g.codecVersion, filter)
			if err != nil {
				log.Warn("failed to marshal bloom filter", "error", err)
				continue
			}

			request := PullGossipRequest{
				Filter: filterBytes,
			}
			msgBytes, err := g.codec.Marshal(g.codecVersion, request)
			if err != nil {
				log.Warn("failed to marshal gossip request", "error", err)
				continue
			}

			for i := 0; i < g.gossipSize; i++ {
				if err := g.client.AppRequestAny(context.TODO(), msgBytes, g.handleResponse); err != nil {
					log.Warn("failed to gossip", "error", err)
					continue
				}
			}
		case <-shutdownChan:
			log.Debug("shutting down gossip")
			return
		}
	}
}

func (g *Gossiper[T, U]) handleResponse(nodeID ids.NodeID, responseBytes []byte, err error) {
	if err != nil {
		log.Debug("failed gossip request", "nodeID", nodeID, "error", err)
		return
	}

	response := PullGossipResponse{}
	if _, err := g.codec.Unmarshal(responseBytes, &response); err != nil {
		log.Debug("failed to unmarshal gossip response", "error", err)
		return
	}

	for _, gossipBytes := range response.GossipBytes {
		gossipable := U(new(T))
		if _, err := g.codec.Unmarshal(gossipBytes, gossipable); err != nil {
			log.Debug("failed to unmarshal gossip", "error", err, "nodeID", nodeID)
			continue
		}

		if err := g.set.Add(gossipable); err != nil {
			log.Debug("failed to add gossip to the known set", "error", err, "nodeID", nodeID, "id", gossipable.GetHash())
			continue
		}
	}
}
