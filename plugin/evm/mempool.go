// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/coreth/metrics"
	"github.com/ethereum/go-ethereum/log"
)

var errNoGasUsed = errors.New("no gas used")

// mempoolMetrics defines the metrics for the atomic mempool
type mempoolMetrics struct {
	pendingTxs     metrics.Gauge   // Gauge of currently pending transactions in the txHeap
	addedTxs       metrics.Counter // Count of all transactions added to the mempool
	newTxsReturned metrics.Counter // Count of transactions returned from GetNewTxs
}

// newMempoolMetrics constructs metrics for the atomic mempool
func newMempoolMetrics() *mempoolMetrics {
	return &mempoolMetrics{
		pendingTxs:     metrics.GetOrRegisterGauge("atomic_mempool_pending_txs", nil),
		addedTxs:       metrics.GetOrRegisterCounter("atomic_mempool_added_txs", nil),
		newTxsReturned: metrics.GetOrRegisterCounter("atomic_mempool_new_txs_returned", nil),
	}
}

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	lock sync.RWMutex

	ctx *snow.Context

	// atomicBackend keeps track of issued transactions
	atomicBackend AtomicBackend
	// maxSize is the maximum number of transactions allowed to be kept in mempool
	maxSize int
	// Pending is a channel of length one, which the mempool ensures has an item on
	// it as long as there is an unissued transaction remaining in [txs]
	Pending chan struct{}
	// newTxs is an array of [Tx] that are ready to be gossiped.
	newTxs []*Tx
	// txHeap is a sorted record of all txs in the mempool by [gasPrice]
	// NOTE: [txHeap] ONLY contains pending txs
	txHeap *txHeap
	// utxoSpenders maps utxoIDs to the transaction consuming them in the mempool
	utxoSpenders map[ids.ID]*Tx
	// bloom is a bloom filter containing the txs in the mempool
	bloom *gossip.BloomFilter

	metrics *mempoolMetrics

	verify func(tx *Tx) error
}

// NewMempool returns a Mempool with [maxSize]
func NewMempool(ctx *snow.Context, maxSize int, atomicBackend AtomicBackend, verify func(tx *Tx) error) (*Mempool, error) {
	bloom, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	return &Mempool{
		ctx:           ctx,
		atomicBackend: atomicBackend,
		Pending:       make(chan struct{}, 1),
		txHeap:        newTxHeap(maxSize),
		maxSize:       maxSize,
		utxoSpenders:  make(map[ids.ID]*Tx),
		bloom:         bloom,
		metrics:       newMempoolMetrics(),
		verify:        verify,
	}, nil
}

// Len returns the number of transactions in the mempool
func (m *Mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.length()
}

// assumes the lock is held
func (m *Mempool) length() int {
	return m.txHeap.Len()
}

// has checks if a given transaction ID [txID] is present in the mempool.
func (m *Mempool) has(txID ids.ID) bool {
	_, found := m.txHeap.Get(txID)
	return found
}

// atomicTxGasPrice is the [gasPrice] paid by a transaction to burn a given
// amount of [AVAXAssetID] given the value of [gasUsed].
func (m *Mempool) atomicTxGasPrice(tx *Tx) (uint64, error) {
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return 0, err
	}
	if gasUsed == 0 {
		return 0, errNoGasUsed
	}
	burned, err := tx.Burned(m.ctx.AVAXAssetID)
	if err != nil {
		return 0, err
	}
	return burned / gasUsed, nil
}

func (m *Mempool) Add(tx *GossipAtomicTx) error {
	m.ctx.Lock.RLock()
	defer m.ctx.Lock.RUnlock()

	return m.AddTx(tx.Tx)
}

// Add attempts to add [tx] to the mempool and returns an error if
// it could not be addeed to the mempool.
func (m *Mempool) AddTx(tx *Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	err := m.addTx(tx, false)
	if err != nil {
		// unlike local txs, invalid remote txs are recorded as discarded
		// so that they won't be requested again
		txID := tx.ID()
		log.Debug("failed to issue remote tx to mempool",
			"txID", txID,
			"err", err,
		)
	}
	return err
}

func (m *Mempool) AddLocalTx(tx *Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.addTx(tx, false)
}

// forceAddTx forcibly adds a *Tx to the mempool and bypasses all verification.
func (m *Mempool) ForceAddTx(tx *Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.addTx(tx, true)
}

// checkConflictTx checks for any transactions in the mempool that spend the same input UTXOs as [tx].
// If any conflicts are present, it returns the highest gas price of any conflicting transaction, the
// txID of the corresponding tx and the full list of transactions that conflict with [tx].
func (m *Mempool) checkConflictTx(tx *Tx) (uint64, ids.ID, []*Tx, error) {
	utxoSet := tx.InputUTXOs()

	var (
		highestGasPrice             uint64 = 0
		conflictingTxs              []*Tx  = make([]*Tx, 0)
		highestGasPriceConflictTxID ids.ID = ids.ID{}
	)
	for utxoID := range utxoSet {
		// Get current gas price of the existing tx in the mempool
		conflictTx, ok := m.utxoSpenders[utxoID]
		if !ok {
			continue
		}
		conflictTxID := conflictTx.ID()
		conflictTxGasPrice, err := m.atomicTxGasPrice(conflictTx)
		// Should never error to calculate the gas price of a transaction already in the mempool
		if err != nil {
			return 0, ids.ID{}, conflictingTxs, fmt.Errorf("failed to re-calculate gas price for conflict tx due to: %w", err)
		}
		if highestGasPrice < conflictTxGasPrice {
			highestGasPrice = conflictTxGasPrice
			highestGasPriceConflictTxID = conflictTxID
		}
		conflictingTxs = append(conflictingTxs, conflictTx)
	}
	return highestGasPrice, highestGasPriceConflictTxID, conflictingTxs, nil
}

// addTx adds [tx] to the mempool. Assumes [m.lock] is held.
// If [force], skips existence and conflict checks within the mempool.
func (m *Mempool) addTx(tx *Tx, force bool) error {
	txID := tx.ID()
	// If [txID] has already been issued or is in the currentTxs map
	// there's no need to add it.
	if !force {
		if _, exists := m.txHeap.Get(txID); exists {
			return nil
		}

		if _, _, err := m.atomicBackend.GetPendingTx(txID); err == nil {
			return nil
		}

		if m.verify != nil {
			if err := m.verify(tx); err != nil {
				return err
			}
		}
	}

	utxoSet := tx.InputUTXOs()
	gasPrice, _ := m.atomicTxGasPrice(tx)
	highestGasPrice, highestGasPriceConflictTxID, conflictingTxs, err := m.checkConflictTx(tx)
	if err != nil {
		return err
	}
	if len(conflictingTxs) != 0 && !force {
		// If [tx] does not have a higher fee than all of its conflicts,
		// we refuse to issue it to the mempool.
		if highestGasPrice >= gasPrice {
			return fmt.Errorf(
				"%w: issued tx (%s) gas price %d <= conflict tx (%s) gas price %d (%d total conflicts in mempool)",
				errConflictingAtomicTx,
				txID,
				gasPrice,
				highestGasPriceConflictTxID,
				highestGasPrice,
				len(conflictingTxs),
			)
		}
		// Remove any conflicting transactions from the mempool
		for _, conflictTx := range conflictingTxs {
			m.removeTx(conflictTx)
		}
	}
	// If adding this transaction would exceed the mempool's size, check if there is a lower priced
	// transaction that can be evicted from the mempool
	if m.length() >= m.maxSize {
		if m.txHeap.Len() > 0 {
			// Get the lowest price item from [txHeap]
			minTx, minGasPrice := m.txHeap.PeekMin()
			// If the [gasPrice] of the lowest item is >= the [gasPrice] of the
			// submitted item, discard the submitted item (we prefer items
			// already in the mempool).
			if minGasPrice >= gasPrice {
				return fmt.Errorf(
					"%w currentMin=%d provided=%d",
					errInsufficientAtomicTxFee,
					minGasPrice,
					gasPrice,
				)
			}

			m.removeTx(minTx)
		} else {
			// This could occur if we have used our entire size allowance on
			// transactions that are currently processing.
			return errTooManyAtomicTx
		}
	}

	// Add the transaction to the [txHeap] so we can evaluate new entries based
	// on how their [gasPrice] compares and add to [utxoSet] to make sure we can
	// reject conflicting transactions.
	m.txHeap.Push(tx, gasPrice)
	m.metrics.addedTxs.Inc(1)
	m.metrics.pendingTxs.Update(int64(m.txHeap.Len()))
	for utxoID := range utxoSet {
		m.utxoSpenders[utxoID] = tx
	}

	m.bloom.Add(&GossipAtomicTx{Tx: tx})
	reset, err := gossip.ResetBloomFilterIfNeeded(m.bloom, txGossipMaxFalsePositiveRate)
	if err != nil {
		return err
	}

	if reset {
		log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

		for _, pendingTx := range m.txHeap.minHeap.items {
			m.bloom.Add(&GossipAtomicTx{Tx: pendingTx.tx})
		}
	}

	// When adding [tx] to the mempool make sure that there is an item in Pending
	// to signal the VM to produce a block. Note: if the VM's buildStatus has already
	// been set to something other than [dontBuild], this will be ignored and won't be
	// reset until the engine calls BuildBlock. This case is handled in IssueCurrentTx
	// and CancelCurrentTx.
	m.newTxs = append(m.newTxs, tx)
	m.addPending()

	return nil
}

func (m *Mempool) Iterate(f func(tx *GossipAtomicTx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	for _, item := range m.txHeap.maxHeap.items {
		if !f(&GossipAtomicTx{Tx: item.tx}) {
			return
		}
	}
}

func (m *Mempool) GetFilter() ([]byte, []byte, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	bloom, err := m.bloom.Bloom.MarshalBinary()
	salt := m.bloom.Salt

	return bloom, salt[:], err
}

func (m *Mempool) GetTxs() []*Tx {
	m.lock.RLock()
	defer m.lock.RUnlock()

	txs := make([]*Tx, 0, m.txHeap.maxHeap.Len())
	for _, pendingTx := range m.txHeap.maxHeap.items {
		txs = append(txs, pendingTx.tx)
	}
	return txs
}

// GetTx retrieves a transaction from the mempool or atomic backend based on its unique [txID].
// If the transaction is found in the backend, it is returned with a blockHeight > 0  and true to indicate processing.
// If the transaction is found in the mempool, it is returned with a blockHeight of 0 and true to indicate processing.
// If the transaction is not found in the backend or mempool, it is not returned, and 'false' is returned instead.
func (m *Mempool) GetTx(txID ids.ID) (*Tx, uint64, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	tx, blockHeight, err := m.atomicBackend.GetPendingTx(txID)
	if err == nil {
		return tx, blockHeight, true
	}

	if tx, ok := m.txHeap.Get(txID); ok {
		return tx, 0, true
	}
	return nil, 0, false
}

// removeTx removes [txID] from the mempool.
// Note: removeTx will delete all entries from [utxoSpenders] corresponding
// to input UTXOs of [txID]. This means that when replacing a conflicting tx,
// removeTx must be called for all conflicts before overwriting the utxoSpenders
// map.
// Assumes lock is held.
func (m *Mempool) removeTx(tx *Tx) {
	txID := tx.ID()
	// Remove from [currentTxs], [txHeap], and [issuedTxs].
	m.txHeap.Remove(txID)
	m.metrics.pendingTxs.Update(int64(m.txHeap.Len()))

	// Remove all entries from [utxoSpenders].
	m.removeSpenders(tx)
}

// removeSpenders deletes the entries for all input UTXOs of [tx] from the
// [utxoSpenders] map.
// Assumes the lock is held.
func (m *Mempool) removeSpenders(tx *Tx) {
	for utxoID := range tx.InputUTXOs() {
		delete(m.utxoSpenders, utxoID)
	}
}

// RemoveTx removes [txID] from the mempool completely.
func (m *Mempool) RemoveTx(tx *Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.removeTx(tx)
}

// RemoveTxs removes all txs from the mempool completely.
func (m *Mempool) RemoveTxs() {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, pendingTx := range m.txHeap.maxHeap.lookup {
		m.removeTx(pendingTx.tx)
	}
}

// addPending makes sure that an item is in the Pending channel.
func (m *Mempool) addPending() {
	select {
	case m.Pending <- struct{}{}:
	default:
	}
}

// GetNewTxs returns the array of [newTxs] and replaces it with an empty array.
func (m *Mempool) GetNewTxs() []*Tx {
	m.lock.Lock()
	defer m.lock.Unlock()

	cpy := m.newTxs
	m.newTxs = nil
	m.metrics.newTxsReturned.Inc(int64(len(cpy))) // Increment the number of newTxs
	return cpy
}
