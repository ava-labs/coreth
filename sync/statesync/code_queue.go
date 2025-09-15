// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"

	syncpkg "github.com/ava-labs/coreth/sync"
)

const (
	defaultMaxOutstandingCodeHashes = 5000
	defaultAutoInit                 = true
)

var (
	_ syncpkg.CodeRequestQueue = (*CodeQueue)(nil)

	errFailedToAddCodeHashesToQueue = errors.New("failed to add code hashes to queue")
	errFailedToFinalizeCodeQueue    = errors.New("failed to finalize code queue")

	// errAddCodeAfterFinalize is returned when [AddCode] is called after [Finalize] has been called.
	// has permanently closed the queue to new work which guards against racy producers
	// trying to enqueue during or after shutdown.
	errAddCodeAfterFinalize = errors.New("cannot add code hashes after finalize")
)

// CodeQueue implements the producer side of code fetching.
// It accepts code hashes, persists "to-fetch" markers, and enqueues hashes
// onto an internal channel consumed by the code syncer.
type CodeQueue struct {
	db ethdb.Database

	// Protects in-memory dedupe (outstanding) and batch selection in addCode.
	dedupeMu sync.Mutex

	// Closed by Init to signal that the queue is ready.
	open chan struct{}

	// Closed by the owner to indicate shutdown and enqueue aborts immediately.
	quit <-chan struct{}

	// Set when Finalize has been called which disallows further AddCode.
	finalized atomic.Bool

	// Consumed by the code syncer workers to fetch code hashes.
	codeHashes chan common.Hash

	// Internal channel for producers - a single forwarder owns codeHashes sends and close.
	// All producers publish to enqueueCh - forwardEnqueues() relays to codeHashes.
	enqueueCh chan common.Hash

	// Set of code hashes enqueued in-memory to avoid duplicate sends.
	outstanding set.Set[common.Hash]

	// Ensures channels are closed exactly once from any shutdown path.
	enqueueCloseOnce sync.Once

	// Synchronize producers with closing of enqueueCh to avoid send-after-close
	// races detected by the race detector. Producers take RLock during sends.
	// Finalize takes Lock before closing enqueueCh.
	sendMu sync.RWMutex
}

type codeQueueOptions struct {
	autoInit       bool
	maxOutstanding int
}

type CodeQueueOption func(*codeQueueOptions)

// WithAutoInit toggles whether Init is called in the constructor.
func WithAutoInit(autoInit bool) CodeQueueOption {
	return func(o *codeQueueOptions) {
		o.autoInit = autoInit
	}
}

// WithMaxOutstandingCodeHashes overrides the queue capacity.
func WithMaxOutstandingCodeHashes(n int) CodeQueueOption {
	return func(o *codeQueueOptions) {
		if n > 0 {
			o.maxOutstanding = n
		}
	}
}

// NewCodeQueue creates a new code queue applying optional functional options.
func NewCodeQueue(db ethdb.Database, done <-chan struct{}, opts ...CodeQueueOption) (*CodeQueue, error) {
	// Apply defaults then options.
	o := codeQueueOptions{
		autoInit:       defaultAutoInit,
		maxOutstanding: defaultMaxOutstandingCodeHashes,
	}
	for _, opt := range opts {
		opt(&o)
	}

	q := &CodeQueue{
		db:          db,
		codeHashes:  make(chan common.Hash, o.maxOutstanding),
		enqueueCh:   make(chan common.Hash, o.maxOutstanding),
		open:        make(chan struct{}),
		outstanding: set.NewSet[common.Hash](0),
		quit:        done,
	}

	if o.autoInit {
		if err := q.Init(); err != nil {
			return nil, err
		}
	}

	return q, nil
}

// CodeHashes returns the receive-only channel of code hashes to consume.
func (q *CodeQueue) CodeHashes() <-chan common.Hash {
	return q.codeHashes
}

// Init enqueues any persisted code markers found on disk and then marks the queue ready.
func (q *CodeQueue) Init() error {
	// Start forwarder that owns codeHashes sends and close.
	// This satisfies the "single closer/sender" rule and avoids send-after-close races.
	go q.forwardEnqueues()

	// Recover any persisted code markers and enqueue them.
	// Note: dbCodeHashes are already present as "to-fetch" markers. addCode will
	// re-persist them, which is a trivial redundancy that happens only on resume
	// (e.g., after restart). We accept this to keep the code simple.
	dbCodeHashes, err := recoverUnfetchedCodeHashes(q.db)
	if err != nil {
		return fmt.Errorf("unable to recover previous sync state: %w", err)
	}
	if err := q.addCode(dbCodeHashes); err != nil {
		return fmt.Errorf("unable to resume previous sync: %w", err)
	}
	close(q.open)

	return nil
}

// AddCode implements [syncpkg.CodeRequestQueue] by persisting and enqueueing new hashes.
// Blocks until the queue is initialized via Init.
//
// Flow:
//  1. Gate on open (Init) and quit (shutdown) so callers don't block forever.
//  2. If finalized, refuse new work to ensure deterministic shutdown.
//  3. Persist "to-fetch" markers (deduped) and publish selected hashes to enqueueCh.
//     The forwarder goroutine owns relaying to codeHashes and closing it.
func (q *CodeQueue) AddCode(codeHashes []common.Hash) error {
	// If shutdown already requested, refuse immediately with shutdown sentinel.
	select {
	case <-q.quit:
		return errFailedToAddCodeHashesToQueue
	default:
	}
	// Block until initialized, but still respect a concurrent shutdown.
	select {
	case <-q.open:
	case <-q.quit:
		return errFailedToAddCodeHashesToQueue
	}

	// If finalized, refuse any new work. This ensures deterministic shutdown
	// and prevents post-finalization enqueues caused by racy producers.
	if q.finalized.Load() {
		return errAddCodeAfterFinalize
	}

	if len(codeHashes) == 0 {
		return nil
	}

	return q.addCode(codeHashes)
}

// Finalize implements [syncpkg.CodeRequestQueue] by signaling no further code hashes will be added.
func (q *CodeQueue) Finalize() error {
	// If shutdown already happened, treat as early-exit. The forwarder will
	// observe quit and close codeHashes. We just report the sentinel here.
	select {
	case <-q.quit:
		// Don't close enqueueCh here - forwarder will exit via quit and close codeHashes.
		return errFailedToFinalizeCodeQueue
	default:
	}

	// Ensure initialization is complete, still respecting shutdown if it happens now.
	select {
	case <-q.open:
	case <-q.quit:
		return errFailedToFinalizeCodeQueue
	}

	// Mark finalized so producers switch to errAddCodeAfterFinalize, then close
	// the internal producer channel. The forwarder will drain remaining items (if any)
	// and close codeHashes exactly once when enqueueCh is closed.
	q.finalized.Store(true)
	q.enqueueCloseOnce.Do(func() {
		q.sendMu.Lock()
		defer q.sendMu.Unlock()
		close(q.enqueueCh)
	})

	return nil
}

func (q *CodeQueue) addCode(codeHashes []common.Hash) error {
	if len(codeHashes) == 0 {
		return nil
	}
	batch := q.db.NewBatch()

	selected := q.filterCodeHashesToFetch(batch, codeHashes)

	if len(selected) == 0 {
		return nil
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers due to: %w", err)
	}

	return q.enqueue(selected)
}

// filterHashesToFetch returns the subset of codeHashes that should be enqueued and
// persists their "to-fetch" markers into the provided batch. This method acquires
// q.lock and is safe to call concurrently from multiple AddCode calls.
func (q *CodeQueue) filterCodeHashesToFetch(batch ethdb.Batch, codeHashes []common.Hash) []common.Hash {
	q.dedupeMu.Lock()
	defer q.dedupeMu.Unlock()

	selected := make([]common.Hash, 0, len(codeHashes))
	for _, codeHash := range codeHashes {
		// Skip if already enqueued in-memory or already have the code locally.
		if q.outstanding.Contains(codeHash) || rawdb.HasCode(q.db, codeHash) {
			continue
		}

		selected = append(selected, codeHash)
		q.outstanding.Add(codeHash)
		customrawdb.AddCodeToFetch(batch, codeHash)
	}

	return selected
}

// enqueue publishes the provided code hashes to the internal channel for the forwarder to relay to codeHashes.
func (q *CodeQueue) enqueue(codeHashes []common.Hash) error {
	// Publish to enqueueCh so the single forwarder can relay to codeHashes
	// and remain the sole closer/sender on that channel.
	q.sendMu.RLock()
	defer q.sendMu.RUnlock()
	for _, h := range codeHashes {
		// Attempt to send, still respecting shutdown while potentially blocking.
		select {
		case q.enqueueCh <- h:
		case <-q.quit:
			return errFailedToAddCodeHashesToQueue
		}
	}

	return nil
}

// forwardEnqueues relays items from enqueueCh to codeHashes and is the sole
// closer of codeHashes to satisfy channel-closing invariants. It also respects
// quit by closing codeHashes as soon as shutdown is signaled.
func (q *CodeQueue) forwardEnqueues() {
	for {
		select {
		case h, ok := <-q.enqueueCh:
			if !ok {
				close(q.codeHashes)
				return
			}
			if !q.forwardOne(h) {
				return
			}
		case <-q.quit:
			close(q.codeHashes)
			return
		}
	}
}

// forwardOne attempts to forward a single hash to codeHashes while respecting quit.
// Returns false if shutdown was observed and the caller should exit.
func (q *CodeQueue) forwardOne(h common.Hash) bool {
	select {
	case q.codeHashes <- h:
		return true
	case <-q.quit:
		close(q.codeHashes)
		return false
	}
}

// recoverUnfetchedCodeHashes cleans out any codeToFetch markers from the database that are no longer
// needed and returns any outstanding markers to the queue.
func recoverUnfetchedCodeHashes(db ethdb.Database) ([]common.Hash, error) {
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	batch := db.NewBatch()
	var codeHashes []common.Hash

	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])

		// If we already have the codeHash, delete the marker from the database and continue.
		if !rawdb.HasCode(db, codeHash) {
			codeHashes = append(codeHashes, codeHash)
			continue
		}

		customrawdb.DeleteCodeToFetch(batch, codeHash)
		if batch.ValueSize() < ethdb.IdealBatchSize {
			continue
		}

		// Write the batch to disk if it has reached the ideal batch size.
		if err := batch.Write(); err != nil {
			return nil, fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
		batch.Reset()
	}

	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("failed to iterate code entries to fetch: %w", err)
	}

	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return nil, fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
	}

	return codeHashes, nil
}
