// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
	"sync"

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

// WithAutoInit toggles whether Init is called in the constructor.
func WithAutoInit(auto bool) func(*codeFetcherOptions) {
	return func(o *codeFetcherOptions) { o.autoInit = auto }
}

// WithMaxOutstandingCodeHashes overrides the queue capacity.
func WithMaxOutstandingCodeHashes(n int) func(*codeFetcherOptions) {
	return func(o *codeFetcherOptions) { o.maxOutstanding = n }
}

// NewCodeFetcherQueue creates a new code fetcher queue applying optional functional options.
func NewCodeFetcherQueue(db ethdb.Database, done <-chan struct{}, opts ...func(*codeFetcherOptions)) (*CodeFetcherQueue, error) {
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
	if len(codeHashes) == 0 {
		return nil
	}
	<-q.open

	return q.addCode(codeHashes)
}

// Finalize implements [synccommon.CodeFetcher] by signaling no further code hashes will be added.
func (q *CodeFetcherQueue) Finalize() {
	<-q.open
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
