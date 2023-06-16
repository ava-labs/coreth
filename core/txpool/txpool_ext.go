// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"math/big"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// PendingSize returns the number of pending txs in the tx pool.
func (pool *TxPool) PendingSize() int {
	pending := pool.Pending(true)
	count := 0
	for _, txs := range pending {
		count += len(txs)
	}
	return count
}

// SetMinFee updates the minimum fee required by the transaction pool for a
// new transaction, and drops all transactions below this threshold.
func (p *TxPool) SetMinFee(tip *big.Int) {
	for _, subpool := range p.subpools {
		subpool.SetMinFee(tip)
	}
}

func (p *TxPool) GasTip() *big.Int {
	return p.subpools[0].GasTip()
}

func (p *TxPool) HasLocal(hash common.Hash) bool {
	for _, subpool := range p.subpools {
		if subpool.HasLocal(hash) {
			return true
		}
	}
	return false
}

func (pool *TxPool) SubscribeNewReorgEvent(ch chan<- core.NewTxPoolReorgEvent) event.Subscription {
	return pool.subs.Track(pool.resetFeed.Subscribe(ch))
}

// TODO: consider removing these wrappers

func (pool *TxPool) AddLocals(txs []*types.Transaction) []error {
	return pool.Add(WrapTxs(txs), true, true)
}

func (pool *TxPool) AddRemotes(txs []*types.Transaction) []error {
	return pool.Add(WrapTxs(txs), false, false)
}

func (pool *TxPool) AddRemotesSync(txs []*types.Transaction) []error {
	return pool.Add(WrapTxs(txs), false, true)
}

func WrapTxs(txs []*types.Transaction) []*Transaction {
	wrapped := make([]*Transaction, len(txs))
	for i, tx := range txs {
		wrapped[i] = &Transaction{Tx: tx}
	}
	return wrapped
}
