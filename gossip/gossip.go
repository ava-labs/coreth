// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/x/p2p"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

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

func (g *Gossiper[T, U]) Pull(
	shutdownChan chan struct{},
	shutdownWg *sync.WaitGroup,
) {
	gossipTicker := time.NewTicker(g.frequency)
	defer func() {
		gossipTicker.Stop()
		shutdownWg.Done()
	}()

	for {
		select {
		case <-gossipTicker.C:
			bloomBytes, err := g.set.GetBloomFilter()
			if err != nil {
				log.Warn("failed to marshal bloom filter", "error", err)
				continue
			}

			request := PullGossipRequest{
				BloomFilter: bloomBytes,
			}
			msgBytes, err := g.codec.Marshal(g.codecVersion, request)
			if err != nil {
				log.Warn("failed to marshal gossip message", "error", err)
				continue
			}

			onResponse := func(nodeID ids.NodeID, responseBytes []byte, err error) {
				if err != nil {
					log.Warn("failed gossip request", "nodeID", nodeID, "error", err)
					return
				}

				response := PullGossipResponse{}
				if _, err := g.codec.Unmarshal(responseBytes, &response); err != nil {
					log.Warn("failed to unmarshal gossip", "error", err)
					return
				}

				for _, gossipBytes := range response.GossipBytes {
					gossipable := U(new(T))
					if err := gossipable.Unmarshal(gossipBytes); err != nil {
						log.Debug("failed to unmarshal transaction", "error", err, "nodeID", nodeID)
						continue
					}

					ok, err := g.set.Add(gossipable)
					if err != nil {
						log.Debug("failed to add gossip to the known set", "error", err, "nodeID", nodeID, "id", gossipable.GetID())
						continue
					}
					if !ok {
						log.Debug("failed to add gossip to the known set", "error", err, "nodeID", nodeID, "id", gossipable.GetID())
						continue
					}
				}
			}

			for i := 0; i < g.gossipSize; i++ {
				if err := g.client.AppRequestAny(context.TODO(), msgBytes, onResponse); err != nil {
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
