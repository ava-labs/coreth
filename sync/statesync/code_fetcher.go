// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
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

var _ synccommon.CodeFetcher = (*CodeFetcherQueue)(nil)

// CodeFetcherQueue implements the producer side of code fetching.
// It accepts code hashes, persists "to-fetch" markers, and enqueues hashes
// onto an internal channel consumed by the code syncer.
type CodeFetcherQueue struct {
	db   ethdb.Database
	lock sync.Mutex
	open chan struct{}
	done <-chan struct{}
	// Set when Finalize has been called which disallows further AddCode.
	finalized atomic.Bool

	// Channel of code hashes to be consumed by the code syncer.
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
	// Apply defaults then options
	o := codeFetcherOptions{
		autoInit:       defaultAutoInit,
		maxOutstanding: defaultMaxOutstandingCodeHashes,
	}
	for _, opt := range opts {
		opt(&o)
	}

	dbCodeHashes, err := getCodeToFetchFromDB(db)
	if err != nil {
		return nil, fmt.Errorf("failed to add code hashes to queue: %w", err)
	}

	q := &CodeFetcherQueue{
		db:           db,
		codeHashes:   make(chan common.Hash, o.maxOutstanding),
		open:         make(chan struct{}),
		outstanding:  set.NewSet[common.Hash](0),
		dbCodeHashes: dbCodeHashes,
		done:         done,
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
	<-q.open
	if q.finalized.Load() {
		return errFailedToAddCodeHashesToQueue
	}
	if len(codeHashes) == 0 {
		return nil
	}

	return q.addCode(codeHashes)
}

// Finalize implements [synccommon.CodeFetcher] by signaling no further code hashes will be added.
func (q *CodeFetcherQueue) Finalize() {
	<-q.open
	q.finalized.Store(true)
	close(q.codeHashes)
}

func (q *CodeFetcherQueue) addCode(codeHashes []common.Hash) error {
	if len(codeHashes) == 0 {
		return nil
	}
	batch := q.db.NewBatch()

	q.lock.Lock()
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
	q.lock.Unlock()

	if len(selected) == 0 {
		return nil
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers due to: %w", err)
	}

	return q.enqueue(selected)
}

func (q *CodeFetcherQueue) enqueue(codeHashes []common.Hash) error {
	for _, codeHash := range codeHashes {
		select {
		case q.codeHashes <- codeHash:
		case <-q.done:
			return errFailedToAddCodeHashesToQueue
		}
	}

	return nil
}

// Clean out any codeToFetch markers from the database that are no longer needed and
// return any outstanding markers to the queue.
func getCodeToFetchFromDB(db ethdb.Database) ([]common.Hash, error) {
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	batch := db.NewBatch()
	codeHashes := make([]common.Hash, 0)
	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		// If we already have the codeHash, delete the marker from the database and continue

		if rawdb.HasCode(db, codeHash) {
			customrawdb.DeleteCodeToFetch(batch, codeHash)
			// Write the batch to disk if it has reached the ideal batch size.
			if batch.ValueSize() > ethdb.IdealBatchSize {
				if err := batch.Write(); err != nil {
					return nil, fmt.Errorf("failed to write batch removing old code markers: %w", err)
				}
				batch.Reset()
			}
			continue
		}

		codeHashes = append(codeHashes, codeHash)
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
