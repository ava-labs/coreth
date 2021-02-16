// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
)

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	lock sync.Mutex
	// maxSize is the maximum number of transactions allowed to be kept in mempool
	maxSize int
	// currentTx is the transaction about to be added to a block.
	currentTx *Tx
	// txs is the set of transactions that need to be issued into new blocks
	txs map[ids.ID]*Tx
	// issuedTxs is the set of transactions that have been issued into a new block
	issuedTxs map[ids.ID]*Tx
	// discardedTxs is an LRU Cache of recently discarded transactions
	discardedTxs *cache.LRU
	// Pending is a channel that signals when the mempool is ready to put an atomic transaction
	// into a new block
	Pending chan struct{}
}

// NewMempool returns a Mempool with [maxSize]
func NewMempool(maxSize int) *Mempool {
	return &Mempool{
		txs:          make(map[ids.ID]*Tx),
		issuedTxs:    make(map[ids.ID]*Tx),
		discardedTxs: &cache.LRU{Size: 10},
		Pending:      make(chan struct{}, 1),
		maxSize:      maxSize,
	}
}

// Len returns the number of transactions in the mempool
func (m *Mempool) Len() int {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.length()
}

// assumes the lock is held
func (m *Mempool) length() int {
	return len(m.txs) + len(m.issuedTxs)
}

// Add attempts to add [tx] to the mempool and returns true if the
// mempool requires a new block to be built.
func (m *Mempool) AddTx(tx *Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	txID := tx.ID()
	// If [txID] has already been issued, there is no need to
	// build a new block.
	if _, exists := m.issuedTxs[txID]; exists {
		return nil
	}

	// Each of the following cases results in there being a
	// pending transaction, so we add to the channel here.
	m.addPending()

	if m.length() >= m.maxSize {
		return errTooManyAtomicTx
	}

	if _, exists := m.txs[txID]; exists {
		return nil
	}

	m.txs[txID] = tx
	return nil
}

// NextTx returns a transaction to be issued from the mempool.
func (m *Mempool) NextTx() (*Tx, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for txID, tx := range m.txs {
		delete(m.txs, txID)
		m.currentTx = tx
		// If there are still atomic transactions remaining
		// ensure there is an item on the pending channel.
		if len(m.txs) > 0 {
			m.addPending()
		}
		return tx, true
	}

	return nil, false
}

// GetTx returns the transaction [txID] if it was issued
// by this node and returns whether it was dropped and whether
// it exists.
func (m *Mempool) GetTx(txID ids.ID) (*Tx, bool, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if tx, ok := m.txs[txID]; ok {
		return tx, false, true
	}
	if tx, ok := m.issuedTxs[txID]; ok {
		return tx, false, true
	}
	if m.currentTx != nil && m.currentTx.ID() == txID {
		return m.currentTx, false, true
	}
	if tx, exists := m.discardedTxs.Get(txID); exists {
		return tx.(*Tx), true, true
	}

	return nil, false, false
}

// IssueCurrentTx marks [currentTx] as issued if there is one
func (m *Mempool) IssueCurrentTx() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx != nil {
		m.issuedTxs[m.currentTx.ID()] = m.currentTx
		m.currentTx = nil
	}

	if len(m.txs) > 0 {
		m.addPending()
	}
}

// CancelCurrentTx marks the attempt to issue [currentTx]
// as being aborted. If this is called after a buildBlock error
// caused by the atomic transaction, then DiscardCurrentTx should have been called
// such that this call will have no effect and should not re-issue the invalid tx.
func (m *Mempool) CancelCurrentTx() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx != nil {
		m.txs[m.currentTx.ID()] = m.currentTx
		m.currentTx = nil
	}

	// If the VM failed to build a block for any reason
	// make sure there is an item on the Pending channel if
	// there are any more transactions to issue.
	if len(m.txs) > 0 {
		m.addPending()
	}
}

// DiscardCurrentTx marks [currentTx] as invalid and aborts the attempt
// to issue it since it failed verification.
func (m *Mempool) DiscardCurrentTx() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx == nil {
		return
	}

	m.discardedTxs.Put(m.currentTx.ID(), m.currentTx)
	m.currentTx = nil
}

// RemoveTx removes [txID] from the mempool completely.
func (m *Mempool) RemoveTx(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx != nil && m.currentTx.ID() == txID {
		m.currentTx = nil
	}
	delete(m.txs, txID)
	delete(m.issuedTxs, txID)
}

// RejectTx marks [txID] as being rejected and attempts to re-issue
// it if it was previously in the mempool.
func (m *Mempool) RejectTx(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	tx, ok := m.issuedTxs[txID]
	if !ok {
		return
	}
	// If the transaction was issued by the mempool, add it back
	// to transactions pending issuance.
	m.txs[txID] = tx
	m.addPending()
}

// addPending makes sure that an item is added to the pending channel
func (m *Mempool) addPending() {
	select {
	case m.Pending <- struct{}{}:
	default:
	}
}
