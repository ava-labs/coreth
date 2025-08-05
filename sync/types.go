// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/core/types"
)

var (
	// ErrWaitBeforeStart is returned when Wait() is called before Start().
	ErrWaitBeforeStart = errors.New("Wait() called before Start() - call Start() first")
	// ErrSyncerAlreadyStarted is returned when Start() is called on a syncer that has already been started.
	ErrSyncerAlreadyStarted = errors.New("syncer already started")
)

// WaitResult carries both a domain error and a cancellation flag.
// This is used to implement a cancellation-aware result pattern which has three logical outcomes:
// 1. Success				-> Err == nil && Cancelled == false.
// 2. Clean cancellation	-> Err == nil && Cancelled == true.
// 3. Hard failure			-> Err != nil && Cancelled == false.
// The Wait phase interprets those as:
// - case 1 and 2 -> if no per-syncer error, proceed (though if every syncer cleanly cancels and the parent ctx is done, we propagate that).
// - case 3 	  -> immediate "sync execution failed" with the wrapped error.
type WaitResult struct {
	Err       error // real sync failure, or nil if OK
	Cancelled bool  // true if we aborted via s.cancel()
}

// Syncer is the common interface for all sync operations.
// This provides a unified interface for atomic state sync and state trie sync.
type Syncer interface {
	// Start begins the sync operation.
	// The sync will respect context cancellation.
	Start(ctx context.Context) error

	// Wait blocks until the sync operation completes or fails.
	// Returns the final error (nil if successful).
	// The sync will respect context cancellation.
	Wait(ctx context.Context) WaitResult
}

// SummaryProvider is an interface for providing state summaries.
type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}

// Extender is an interface that allows for extending the state sync process.
type Extender interface {
	// CreateSyncer creates a syncer instance for the given client, database, and summary.
	CreateSyncer(ctx context.Context, client syncclient.LeafClient, verDB *versiondb.Database, summary message.Syncable) (Syncer, error)

	// OnFinishBeforeCommit is called before committing the sync results.
	OnFinishBeforeCommit(lastAcceptedHeight uint64, summary message.Syncable) error

	// OnFinishAfterCommit is called after committing the sync results.
	OnFinishAfterCommit(summaryHeight uint64) error
}

// WaitForCompletion blocks until either:
//  1. doneCh yields an error -> Err=that error, Cancelled=false
//  2. ctx is done            -> call cancelFunc(), drain doneCh, then Err=nil, Cancelled=true
//
// If cancelFunc is nil (meaning Start was not called), it returns ErrWaitBeforeStart.
func WaitForCompletion(
	ctx context.Context,
	doneCh <-chan error,
	cancelFunc context.CancelFunc,
) WaitResult {
	// This should only be called after Start, so we can assume cancelFunc is set.
	if cancelFunc == nil {
		return WaitResult{Err: ErrWaitBeforeStart}
	}

	select {
	case err := <-doneCh:
		// Background errgroup finished.
		return WaitResult{Err: err, Cancelled: false}
	case <-ctx.Done():
		// Caller cancelled: tell the syncer to shut down.
		cancelFunc()
		// Wait for the syncer to finish.
		<-doneCh
		// Signal "clean" cancellation.
		return WaitResult{Err: nil, Cancelled: true}
	}
}
