// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/coreth/mempool"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

type Gossiper[Tx mempool.Tx] interface {
	Mempool() mempool.Mempool[Tx]
	GossipRequest() message.MempoolTxsRequest
	Tx() Tx
}

func NewPullGossiper[Tx mempool.Tx](
	gossiper Gossiper[Tx],
	networkClient peer.NetworkClient,
	codec codec.Manager,
	gossipFrequency time.Duration,
	gossipSize int,
	shutdown chan struct{},
	shutdownWg *sync.WaitGroup,
) *PullGossiper[Tx] {
	return &PullGossiper[Tx]{
		gossiper:        gossiper,
		client:          networkClient,
		codec:           codec,
		gossipFrequency: gossipFrequency,
		gossipSize:      gossipSize,
		shutdown:        shutdown,
		shutdownWg:      shutdownWg,
	}
}

type PullGossiper[Tx mempool.Tx] struct {
	gossiper        Gossiper[Tx]
	client          peer.NetworkClient
	codec           codec.Manager
	gossipFrequency time.Duration
	gossipSize      int
	shutdown        chan struct{}
	shutdownWg      *sync.WaitGroup
}

// TODO propagate fatal events instead of just logging them
func (p *PullGossiper[Tx]) Start() {
	p.shutdownWg.Add(1)
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

			for nodeID := range peers {
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
					return
				}
			}
		case <-p.shutdown:
			return
		}
	}
}
