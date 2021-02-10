// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "github.com/ava-labs/avalanchego/ids"

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	maxSize int
	txs     map[ids.ID]*Tx
}

// NewMempool returns a Mempool with [maxSize]
func NewMempool(maxSize int) *Mempool {
	return &Mempool{
		txs:     make(map[ids.ID]*Tx),
		maxSize: maxSize,
	}
}

// Add attempts to add [tx] to the mempool and returns true
// if [tx] has been added to the mempool.
func (m *Mempool) Add(tx *Tx) bool {
	if m.Len() >= m.maxSize {
		return false
	}
	txID := tx.ID()
	if _, exists := m.txs[txID]; exists {
		return true
	}
	m.txs[txID] = tx
	return true
}

// Len returns the number of transactions in the mempool
func (m *Mempool) Len() int { return len(m.txs) }

// Remove attempts to remove a transaction from the mempool
func (m *Mempool) Remove() (*Tx, bool) {
	for txID, tx := range m.txs {
		delete(m.txs, txID)
		return tx, true
	}
	return nil, false
}
