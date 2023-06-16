// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

type Gossiper[T Tx] interface {
	Mempool() Mempool[T]
	GossipRequest() message.MempoolTxsRequest
	Tx() T
}

func NewPullGossiper[T Tx](
	gossiper Gossiper[T],
	networkClient peer.NetworkClient,
	codec codec.Manager,
	gossipFrequency time.Duration,
	gossipSize int,
	shutdown chan struct{},
	shutdownWg *sync.WaitGroup,
) *PullGossiper[T] {
	return &PullGossiper[T]{
		gossiper:        gossiper,
		client:          networkClient,
		codec:           codec,
		gossipFrequency: gossipFrequency,
		gossipSize:      gossipSize,
		shutdown:        shutdown,
		shutdownWg:      shutdownWg,
	}
}

type PullGossiper[T Tx] struct {
	gossiper        Gossiper[T]
	client          peer.NetworkClient
	codec           codec.Manager
	gossipFrequency time.Duration
	gossipSize      int
	shutdown        chan struct{}
	shutdownWg      *sync.WaitGroup
}

// TODO propagate fatal events instead of just logging them
func (p *PullGossiper[T]) Start() {
	gossipTicker := time.NewTicker(p.gossipFrequency)

	defer func() {
		gossipTicker.Stop()
		p.shutdownWg.Done()
	}()

	for {
		select {
		case <-gossipTicker.C:
			peers, err := p.client.Sample(p.gossipSize)
			if err != nil {
				log.Error("failed to sample peers", "error", err)
				return
			}

			for nodeID := range peers {
				bloom := p.gossiper.Mempool().GetPendingTxsBloomFilter()
				bloomBytes, err := bloom.MarshalBinary()
				if err != nil {
					log.Error("failed to marshal bloom filter", "error", err)
					continue
				}

				request := p.gossiper.GossipRequest()
				request.SetBloomFilter(bloomBytes)
				requestBytes, err := message.RequestToBytes(p.codec, request)
				if err != nil {
					log.Error("failed to marshal gossip request", "error", err)
					continue
				}

				responseBytes, err := p.client.SendAppRequest(nodeID, requestBytes)
				if err != nil {
					log.Debug("failed to send gossip request to peer", "error", err, "nodeID", nodeID)
					continue
				}

				var gossipResponse message.MempoolTxsResponse
				if _, err := p.codec.Unmarshal(responseBytes, &gossipResponse); err != nil {
					log.Debug("failed to unmarshal gossip response", "error", err, "nodeID", nodeID)
					continue
				}

				for _, txBytes := range gossipResponse.Txs {
					tx := p.gossiper.Tx()
					if err := tx.Unmarshal(txBytes); err != nil {
						log.Debug("failed to unmarshal transaction", "error", err, "nodeID", nodeID)
						continue
					}

					if err := p.gossiper.Mempool().AddTx(tx); err != nil {
						log.Debug("failed to add transaction to the mempool", "error", err, "nodeID", nodeID, "hash", tx.Hash())
						continue
					}
				}
			}
		case <-p.shutdown:
			return
		}
	}
}
