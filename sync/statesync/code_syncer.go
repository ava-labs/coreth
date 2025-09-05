// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/message"

	synccommon "github.com/ava-labs/coreth/sync"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
)

const defaultNumCodeFetchingWorkers = 5

var _ synccommon.Syncer = (*CodeSyncer)(nil)

// CodeSyncer syncs code bytes from the network in a separate thread.
// Tracks outstanding requests in the DB, so that it will still fulfill them if interrupted.
type CodeSyncer struct {
	db     ethdb.Database
	client statesyncclient.Client
	// Channel of incoming code hash requests provided by the fetcher.
	codeHashes <-chan common.Hash

	// Config options.
	numWorkers          int
	maxCodeHashesPerReq int
}

// Name returns the human-readable name for this sync task.
func (*CodeSyncer) Name() string { return "Code Syncer" }

// ID returns the stable identifier for this sync task.
func (*CodeSyncer) ID() string { return "state_code_sync" }

// CodeSyncerOption configures CodeSyncer at construction time.
type CodeSyncerOption func(*CodeSyncer)

// WithNumCodeFetchingWorkers overrides the number of concurrent workers.
func WithNumCodeFetchingWorkers(n int) CodeSyncerOption {
	return func(c *CodeSyncer) {
		if n > 0 {
			c.numWorkers = n
		}
	}
}

// WithMaxCodeHashesPerRequest overrides the batching size per request.
func WithMaxCodeHashesPerRequest(n int) CodeSyncerOption {
	return func(c *CodeSyncer) {
		if n > 0 {
			c.maxCodeHashesPerReq = n
		}
	}
}

// NewCodeSyncer allows external packages (e.g., registry wiring) to create a code syncer
// that consumes hashes from a provided fetcher queue.
func NewCodeSyncer(client statesyncclient.Client, db ethdb.Database, codeHashes <-chan common.Hash, opts ...CodeSyncerOption) (*CodeSyncer, error) {
	c := &CodeSyncer{
		db:                  db,
		client:              client,
		codeHashes:          codeHashes,
		numWorkers:          defaultNumCodeFetchingWorkers,
		maxCodeHashesPerReq: message.MaxCodeHashesPerRequest,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Sync starts the worker thread and populates the code hashes queue with active work.
// Blocks until all outstanding code requests from a previous sync have been
// fetched and the code channel has been closed, or the context is cancelled.
func (c *CodeSyncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)

	// Start NumCodeFetchingWorkers threads to fetch code from the network.
	for i := 0; i < c.numWorkers; i++ {
		eg.Go(func() error { return c.work(egCtx) })
	}

	return eg.Wait()
}

// work fulfills any incoming requests from the producer channel by fetching code bytes from the network
// and fulfilling them by updating the database.
func (c *CodeSyncer) work(ctx context.Context) error {
	codeHashes := make([]common.Hash, 0, message.MaxCodeHashesPerRequest)

	for {
		select {
		case <-ctx.Done(): // If ctx is done, set the error to the ctx error since work has been cancelled.
			return ctx.Err()
		case codeHash, ok := <-c.codeHashes:
			// If there are no more [codeHashes], fulfill a last code request for any [codeHashes] previously
			// read from the channel, then return.
			if !ok {
				if len(codeHashes) > 0 {
					return c.fulfillCodeRequest(ctx, codeHashes)
				}
				return nil
			}

			codeHashes = append(codeHashes, codeHash)
			// Try to wait for at least [maxCodeHashesPerReq] code hashes to batch into a single request
			// if there's more work remaining.
			if len(codeHashes) < c.maxCodeHashesPerReq {
				continue
			}
			if err := c.fulfillCodeRequest(ctx, codeHashes); err != nil {
				return err
			}

			// Reset the codeHashes array
			codeHashes = codeHashes[:0]
		}
	}
}

// fulfillCodeRequest sends a request for [codeHashes], writes the result to the database, and
// marks the work as complete.
// codeHashes should not be empty or contain duplicate hashes.
// Returns an error if one is encountered, signaling the worker thread to terminate.
func (c *CodeSyncer) fulfillCodeRequest(ctx context.Context, codeHashes []common.Hash) error {
	codeByteSlices, err := c.client.GetCode(ctx, codeHashes)
	if err != nil {
		return err
	}

	batch := c.db.NewBatch()
	for i, codeHash := range codeHashes {
		customrawdb.DeleteCodeToFetch(batch, codeHash)
		rawdb.WriteCode(batch, codeHash, codeByteSlices[i])
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch for fulfilled code requests: %w", err)
	}
	return nil
}
