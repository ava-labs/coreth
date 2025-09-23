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

// closeState models the lifecycle of the output channel.
// It is intentionally minimal and used only within [CodeQueue].
type closeState uint32

const (
	defaultQueueCapacity = 5000

	closeStateOpen      closeState = 0
	closeStateFinalized closeState = 1
	closeStateQuit      closeState = 2
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
	db   ethdb.Database
	quit <-chan struct{}

	// Closed by [CodeQueue.Finalize] after the WaitGroup unblocks.
	out       chan common.Hash
	enqueueWG sync.WaitGroup

	// Indicates why/if the output channel was closed.
	closed atomicCloseState

	// Protects in-memory dedupe (outstanding) and batch selection in addCode.
	dedupeMu sync.Mutex
	// Set of code hashes enqueued in-memory to avoid duplicate sends.
	outstanding set.Set[common.Hash]
}

// TODO: this will be migrated to using libevm's options pattern in a follow-up PR.
type codeQueueOptions struct {
	capacity int
}

type CodeQueueOption func(*codeQueueOptions)

// WithCapacity overrides the queue buffer capacity.
func WithCapacity(n int) CodeQueueOption {
	return func(o *codeQueueOptions) {
		if n > 0 {
			o.capacity = n
		}
	}
}

// NewCodeQueue creates a new code queue applying optional functional options.
func NewCodeQueue(db ethdb.Database, quit <-chan struct{}, opts ...CodeQueueOption) (*CodeQueue, error) {
	// Apply defaults then options.
	o := codeQueueOptions{
		capacity: defaultQueueCapacity,
	}
	for _, opt := range opts {
		opt(&o)
	}

	q := &CodeQueue{
		db:          db,
		out:         make(chan common.Hash, o.capacity),
		outstanding: set.NewSet[common.Hash](0),
		quit:        quit,
	}

	// Close the output channel on early shutdown to unblock consumers.
	go func() {
		<-q.quit
		// If not already closed, mark as quit and close channel.
		q.closed.MarkQuitAndClose(q.out)
	}()

	// Always initialize eagerly.
	if err := q.Init(); err != nil {
		return nil, err
	}

	return q, nil
}

// CodeHashes returns the receive-only channel of code hashes to consume.
func (q *CodeQueue) CodeHashes() <-chan common.Hash {
	return q.out
}

// Init enqueues any persisted code markers found on disk and then marks the queue ready.
func (q *CodeQueue) Init() error {
	// Recover any persisted code markers and enqueue them.
	// Note: dbCodeHashes are already present as "to-fetch" markers. addCode will
	// re-persist them, which is a trivial redundancy that happens only on resume
	// (e.g., after restart). We accept this to keep the code simple.
	dbCodeHashes, err := recoverUnfetchedCodeHashes(q.db)
	if err != nil {
		return fmt.Errorf("unable to recover previous sync state: %w", err)
	}
	if err := q.enqueue(dbCodeHashes); err != nil {
		return fmt.Errorf("unable to resume previous sync: %w", err)
	}

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
	// If the queue has been closed, reject new work.
	switch q.closed.Get() {
	case closeStateFinalized:
		return errAddCodeAfterFinalize
	case closeStateQuit:
		return errFailedToAddCodeHashesToQueue
	}
	return q.enqueue(codeHashes)
}

func (q *CodeQueue) enqueue(codeHashes []common.Hash) error {
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

	q.enqueueWG.Add(1)
	defer q.enqueueWG.Done()
	for _, hash := range selected {
		select {
		case q.out <- hash:
		case <-q.quit:
			return errFailedToAddCodeHashesToQueue
		}
	}
	return nil
}

// Finalize implements [syncpkg.CodeRequestQueue] by signaling no further code hashes will be added.
func (q *CodeQueue) Finalize() error {
	q.enqueueWG.Wait()
	// If already closed due to quit, report early-exit error.
	if q.closed.Get() == closeStateQuit {
		return errFailedToFinalizeCodeQueue
	}
	// Mark as finalized and close the channel (no-op if already finalized).
	q.closed.MarkFinalizedAndClose(q.out)
	return nil
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

// atomicCloseState provides a tiny typed wrapper around an atomic uint32.
// It exposes enum-like operations to make intent explicit at call sites.
type atomicCloseState struct {
	v atomic.Uint32
}

func (s *atomicCloseState) Get() closeState {
	return closeState(s.v.Load())
}

func (s *atomicCloseState) Transition(oldState, newState closeState) bool {
	return s.v.CompareAndSwap(uint32(oldState), uint32(newState))
}

func (s *atomicCloseState) MarkQuitAndClose(out chan common.Hash) {
	if s.Transition(closeStateOpen, closeStateQuit) {
		close(out)
	}
}

func (s *atomicCloseState) MarkFinalizedAndClose(out chan common.Hash) {
	if s.Transition(closeStateOpen, closeStateFinalized) {
		close(out)
	}
}
