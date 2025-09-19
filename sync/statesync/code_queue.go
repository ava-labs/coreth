// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"
	"fmt"
	"sync"

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
	db   ethdb.Database
	quit <-chan struct{}

	out       chan common.Hash // closed by [CodeQueue.Finalize] after the WaitGroup unblocks
	enqueueWG sync.WaitGroup

	// Protects in-memory dedupe (outstanding) and batch selection in addCode.
	dedupeMu sync.Mutex
	// Set of code hashes enqueued in-memory to avoid duplicate sends.
	outstanding set.Set[common.Hash]
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
func NewCodeQueue(db ethdb.Database, quit <-chan struct{}, opts ...CodeQueueOption) (*CodeQueue, error) {
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
		out:         make(chan common.Hash, o.maxOutstanding),
		outstanding: set.NewSet[common.Hash](0),
		quit:        quit,
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
	close(q.out)
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
