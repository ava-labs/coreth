// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/gossip"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

var (
	_ gossip.Gossipable        = (*GossipEthTx)(nil)
	_ gossip.Gossipable        = (*GossipAtomicTx)(nil)
	_ gossip.Set[*GossipEthTx] = (*GossipEthTxPool)(nil)
)

type GossipAtomicTx struct {
	Tx *Tx `serialize:"true"`
}

func (tx *GossipAtomicTx) GetHash() gossip.Hash {
	id := tx.Tx.ID()
	return gossip.HashFromBytes(id[:])
}

func (tx *GossipAtomicTx) Marshal() ([]byte, error) {
	return Codec.Marshal(message.Version, tx)
}

func (tx *GossipAtomicTx) Unmarshal(bytes []byte) error {
	_, err := Codec.Unmarshal(bytes, tx)
	return err
}

func NewGossipEthTxPool(mempool *txpool.TxPool) (*GossipEthTxPool, error) {
	bloom, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	g := &GossipEthTxPool{
		mempool:    mempool,
		pendingTxs: make(chan core.NewTxsEvent),
		bloom:      bloom,
	}
	return g, nil
}

type GossipEthTxPool struct {
	mempool    *txpool.TxPool
	pendingTxs chan core.NewTxsEvent

	bloom *gossip.BloomFilter
	lock  sync.RWMutex
}

func (g *GossipEthTxPool) Subscribe(shutdownChan chan struct{}, shutdownWg *sync.WaitGroup) {
	defer shutdownWg.Done()
	g.mempool.SubscribeNewTxsEvent(g.pendingTxs)

	for {
		select {
		case <-shutdownChan:
			log.Debug("shutting down subscription")
			return
		case pendingTxs := <-g.pendingTxs:
			g.lock.Lock()
			for _, pendingTx := range pendingTxs.Txs {
				tx := &GossipEthTx{Tx: pendingTx}
				g.bloom.Add(tx)
				if gossip.ResetBloomFilterIfNeeded(g.bloom, txGossipBloomMaxFilledRatio) {
					log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

					pending, _ := g.mempool.Content()
					for _, pendingTxs := range pending {
						for _, pendingTx := range pendingTxs {
							g.bloom.Add(&GossipEthTx{Tx: pendingTx})
						}
					}
				}
			}
			g.lock.Unlock()
		}
	}
}

// Add enqueues the transaction to the mempool. Subscribe should be called
// to receive an event if tx is actually added to the mempool or not.
func (g *GossipEthTxPool) Add(tx *GossipEthTx) error {
	if err := g.mempool.AddRemotes([]*types.Transaction{tx.Tx})[0]; err != nil {
		return err
	}

	return nil
}

func (g *GossipEthTxPool) Get(filter func(tx *GossipEthTx) bool) []*GossipEthTx {
	pending, _ := g.mempool.Content()
	result := make([]*GossipEthTx, 0)

	for _, txs := range pending {
		for _, tx := range txs {
			gossipTx := &GossipEthTx{Tx: tx}
			if !filter(gossipTx) {
				continue
			}

			result = append(result, gossipTx)
		}
	}

	return result
}

func (g *GossipEthTxPool) GetFilter() *gossip.BloomFilter {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom
}

type GossipEthTx struct {
	Tx *types.Transaction
}

func (tx *GossipEthTx) GetHash() gossip.Hash {
	hash := tx.Tx.Hash()
	return gossip.HashFromBytes(hash[:])
}

func (tx *GossipEthTx) Marshal() ([]byte, error) {
	return tx.Tx.MarshalBinary()
}

func (tx *GossipEthTx) Unmarshal(bytes []byte) error {
	tx.Tx = &types.Transaction{}
	return tx.Tx.UnmarshalBinary(bytes)
}
