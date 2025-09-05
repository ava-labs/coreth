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

	synccommon "github.com/ava-labs/coreth/sync"
)

const (
	defaultMaxOutstandingCodeHashes = 5000
	defaultAutoInit                 = true
)

var (
	_ synccommon.CodeFetcher = (*CodeFetcherQueue)(nil)

	errFailedToAddCodeHashesToQueue = errors.New("failed to add code hashes to queue")
	errFailedToFinalizeCodeFetcher  = errors.New("failed to finalize code fetcher")

	// errAddCodeAfterFinalize is returned when [AddCode] is called after [Finalize] has been called.
	// has permanently closed the queue to new work which guards against racy producers
	// trying to enqueue during or after shutdown.
	errAddCodeAfterFinalize = errors.New("cannot add code hashes after finalize")
)

// CodeFetcherQueue implements the producer side of code fetching.
// It accepts code hashes, persists "to-fetch" markers, and enqueues hashes
// onto an internal channel consumed by the code syncer.
type CodeFetcherQueue struct {
	db ethdb.Database
	// Guards in-memory dedupe and batch selection in addCode.
	lock sync.Mutex
	// Closed by Init to signal that the queue is ready.
	open chan struct{}
	// Closed by the owner to indicate shutdown and enqueue aborts immediately.
	quit <-chan struct{}
	// Set when Finalize has been called which disallows further AddCode.
	finalized atomic.Bool

	// Tracks in-flight AddCode calls to coordinate clean shutdown.
	addWG sync.WaitGroup

	// Consumed by the code syncer workers to fetch code hashes.
	codeHashes chan common.Hash

	// Set of code hashes enqueued in-memory to avoid duplicate sends.
	outstanding set.Set[common.Hash]

	// Any persisted code markers found on startup to re-enqueue when initialized.
	dbCodeHashes []common.Hash
}

type codeFetcherOptions struct {
	autoInit       bool
	maxOutstanding int
}

type CodeFetcherOption func(*codeFetcherOptions)

// WithAutoInit toggles whether Init is called in the constructor.
func WithAutoInit(autoInit bool) CodeFetcherOption {
	return func(o *codeFetcherOptions) {
		o.autoInit = autoInit
	}
}

// WithMaxOutstandingCodeHashes overrides the queue capacity.
func WithMaxOutstandingCodeHashes(n int) CodeFetcherOption {
	return func(o *codeFetcherOptions) {
		if n > 0 {
			o.maxOutstanding = n
		}
	}
}

// NewCodeFetcherQueue creates a new code fetcher queue applying optional functional options.
func NewCodeFetcherQueue(db ethdb.Database, done <-chan struct{}, opts ...CodeFetcherOption) (*CodeFetcherQueue, error) {
	// Apply defaults then options.
	o := codeFetcherOptions{
		autoInit:       defaultAutoInit,
		maxOutstanding: defaultMaxOutstandingCodeHashes,
	}
	for _, opt := range opts {
		opt(&o)
	}

	dbCodeHashes, err := recoverUnfetchedCodeHashes(db)
	if err != nil {
		return nil, fmt.Errorf("failed to add code hashes to queue: %w", err)
	}

	q := &CodeFetcherQueue{
		db:           db,
		codeHashes:   make(chan common.Hash, o.maxOutstanding),
		open:         make(chan struct{}),
		outstanding:  set.NewSet[common.Hash](0),
		dbCodeHashes: dbCodeHashes,
		quit:         done,
	}

	if o.autoInit {
		if err := q.Init(); err != nil {
			return nil, err
		}
	}

	return q, nil
}

// CodeHashes returns the receive-only channel of code hashes to consume.
func (q *CodeFetcherQueue) CodeHashes() <-chan common.Hash {
	return q.codeHashes
}

// Init enqueues any persisted code markers found on disk and then marks the fetcher ready.
func (q *CodeFetcherQueue) Init() error {
	// Note: dbCodeHashes are already present as "to-fetch" markers. addCode will
	// re-persist them, which is a trivial redundancy that happens only on resume
	// (e.g., after restart). We accept this to keep the code simple.
	if err := q.addCode(q.dbCodeHashes); err != nil {
		return fmt.Errorf("unable to resume previous sync: %w", err)
	}
	q.dbCodeHashes = nil
	close(q.open)

	return nil
}

// AddCode implements [synccommon.CodeFetcher] by persisting and enqueueing new hashes.
// Blocks until the queue is initialized via Init.
func (q *CodeFetcherQueue) AddCode(codeHashes []common.Hash) error {
	select {
	case <-q.open:
	case <-q.quit:
		return errFailedToAddCodeHashesToQueue
	}

	q.addWG.Add(1)
	defer q.addWG.Done()

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

// Finalize implements [synccommon.CodeFetcher] by signaling no further code hashes will be added.
func (q *CodeFetcherQueue) Finalize() error {
	select {
	case <-q.open:
	case <-q.quit:
		return errFailedToFinalizeCodeFetcher
	}

	q.finalized.Store(true)
	// Wait for any in-flight AddCode calls to complete before closing the channel.
	q.addWG.Wait()
	close(q.codeHashes)

	return nil
}

func (q *CodeFetcherQueue) addCode(codeHashes []common.Hash) error {
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
func (q *CodeFetcherQueue) filterCodeHashesToFetch(batch ethdb.Batch, codeHashes []common.Hash) []common.Hash {
	q.lock.Lock()
	defer q.lock.Unlock()

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

func (q *CodeFetcherQueue) enqueue(codeHashes []common.Hash) error {
	for _, h := range codeHashes {
		// Attempt to send, still respecting shutdown while potentially blocking.
		select {
		case q.codeHashes <- h:
		case <-q.quit:
			return errFailedToAddCodeHashesToQueue
		}
	}

	return nil
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
