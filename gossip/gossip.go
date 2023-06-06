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
	"github.com/ava-labs/avalanchego/x/sdk/p2p"
)

type TxConstraint[T any] interface {
	*T
	Tx
}

func NewGossiper[T any, U TxConstraint[T]](
	mempool Mempool[U],
	client *p2p.Client,
	codec codec.Manager,
	codecVersion uint16,
	gossipSize int,
	frequency time.Duration,
) *Gossiper[T, U] {
	return &Gossiper[T, U]{
		mempool:      mempool,
		client:       client,
		codec:        codec,
		codecVersion: codecVersion,
		gossipSize:   gossipSize,
		frequency:    frequency,
	}
}

type Gossiper[T any, U TxConstraint[T]] struct {
	mempool      Mempool[U]
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
			bloom, err := g.mempool.GetBloomFilter()
			if err != nil {
				log.Warn("failed to marshal bloom filter", "error", err)
				continue
			}

			request := PullTxsRequest{
				BloomFilter: bloom,
			}
			msgBytes, err := g.codec.Marshal(g.codecVersion, request)
			if err != nil {
				log.Warn("failed to marshal gossip message", "error", err)
				continue
			}

			onResponse := func(nodeID ids.NodeID, responseBytes []byte, err error) {
				if err != nil {
					log.Warn("failed to pull txs", "nodeID", nodeID, "error", err)
					return
				}

				response := PullTxsResponse{}
				if _, err := g.codec.Unmarshal(responseBytes, &response); err != nil {
					log.Warn("failed to unmarshal txs", "error", err)
					return
				}

				for _, txBytes := range response.Txs {
					tx := U(new(T))
					if err := tx.Unmarshal(txBytes); err != nil {
						log.Debug("failed to unmarshal transaction", "error", err, "nodeID", nodeID)
						continue
					}

					ok, err := g.mempool.AddTx(tx, true)
					if err != nil {
						log.Debug("failed to add transaction to the mempool", "error", err, "nodeID", nodeID, "id", tx.ID())
						continue
					}
					if !ok {
						log.Debug("failed to add transaction to the mempool", "error", err, "nodeID", nodeID, "id", tx.ID())
						continue
					}
				}
			}

			for i := 0; i < g.gossipSize; i++ {
				if err := g.client.AppRequestAny(context.TODO(), msgBytes, onResponse); err != nil {
					log.Warn("failed to gossip txs", "error", err)
					continue
				}
			}
		case <-shutdownChan:
			log.Debug("shutting down tx gossip")
			return
		}
	}
}
