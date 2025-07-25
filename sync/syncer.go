// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
)

// ErrWaitBeforeStart is returned when Wait() is called before Start().
var ErrWaitBeforeStart = errors.New("Wait() called before Start() - call Start() first")

// Syncer is the common interface for all sync operations.
// This provides a unified interface for atomic state sync and state trie sync.
type Syncer interface {
	// Start begins the sync operation.
	// The sync will respect context cancellation.
	Start(ctx context.Context) error

	// Wait blocks until the sync operation completes or fails.
	// Returns the final error (nil if successful).
	// The sync will respect context cancellation.
	Wait(ctx context.Context) error
}

// Extender is an interface that allows for extending the state sync process.
type Extender interface {
	// OnFinishBeforeCommit is called after the state sync process has completed but before the state sync summary is committed.
	OnFinishBeforeCommit(lastAcceptedHeight uint64, syncSummary message.Syncable) error
	// OnFinishAfterCommit is called after the state sync process has completed and the state sync summary is committed.
	OnFinishAfterCommit(summaryHeight uint64) error
}

// SummaryProvider is an interface for providing state summaries.
type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}

// RunSync executes the complete sync operation for a given syncer.
// This encapsulates the entire lifecycle: create syncer → start → wait.
func RunSync(ctx context.Context, operationName string, syncer Syncer, client syncclient.Client, verDB *versiondb.Database, summary message.Syncable) error {
	log.Info(operationName+" starting", "summary", summary)

	// Start syncer
	if err := syncer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start %s: %w", operationName, err)
	}

	// Wait for completion
	err := syncer.Wait(ctx)
	log.Info(operationName+" finished", "summary", summary, "err", err)

	return err
}
