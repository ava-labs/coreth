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

type Config struct {
	Frequency time.Duration
	PollSize  int
}

func NewGossiper[T any, U GossipableAny[T]](
	config Config,
	set Set[U],
	client *p2p.Client,
	codec codec.Manager,
	codecVersion uint16,
) *Gossiper[T, U] {
	return &Gossiper[T, U]{
		config:       config,
		set:          set,
		client:       client,
		codec:        codec,
		codecVersion: codecVersion,
	}
}

type Gossiper[T any, U GossipableAny[T]] struct {
	config       Config
	set          Set[U]
	client       *p2p.Client
	codec        codec.Manager
	codecVersion uint16
}

func (g *Gossiper[T, U]) Gossip(shutdownChan chan struct{}, shutdownWg *sync.WaitGroup) {
	gossipTicker := time.NewTicker(g.config.Frequency)
	defer func() {
		gossipTicker.Stop()
		shutdownWg.Done()
	}()

	for {
		select {
		case <-gossipTicker.C:
			if err := g.gossip(); err != nil {
				log.Warn("failed to gossip", "error", err)
			}
		case <-shutdownChan:
			log.Debug("shutting down gossip")
			return
		}
	}
}

func (g *Gossiper[T, U]) gossip() error {
	filter := g.set.GetFilter()
	bloomBytes, err := filter.Bloom.MarshalBinary()
	if err != nil {
		return err
	}

	request := PullGossipRequest{
		FilterBytes: bloomBytes,
		SaltBytes:   filter.Salt,
	}
	msgBytes, err := g.codec.Marshal(g.codecVersion, request)
	if err != nil {
		return err
	}

	for i := 0; i < g.config.PollSize; i++ {
		if err := g.client.AppRequestAny(context.TODO(), msgBytes, g.handleResponse); err != nil {
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

	response := PullGossipResponse{}
	if _, err := g.codec.Unmarshal(responseBytes, &response); err != nil {
		log.Debug("failed to unmarshal gossip response", "error", err)
		return
	}

	for _, gossipBytes := range response.GossipBytes {
		gossipable := U(new(T))
		if err := gossipable.Unmarshal(gossipBytes); err != nil {
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
