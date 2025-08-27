// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/plugin/evm/message"

	syncclient "github.com/ava-labs/coreth/sync/client"
)

// Syncer is the common interface for all sync operations.
// This provides a unified interface for atomic state sync and state trie sync.
type Syncer interface {
	// Sync completes the full sync operation, returning any errors encountered.
	// The sync will respect context cancellation.
	Sync(ctx context.Context) error
}

// SummaryProvider is an interface for providing state summaries.
type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}

// Extender is an interface that allows for extending the state sync process.
type Extender interface {
	// CreateSyncer creates a syncer instance for the given client, database, and summary.
	CreateSyncer(client syncclient.LeafClient, verDB *versiondb.Database, summary message.Syncable) (Syncer, error)

	// OnFinishBeforeCommit is called before committing the sync results.
	OnFinishBeforeCommit(lastAcceptedHeight uint64, summary message.Syncable) error

	// OnFinishAfterCommit is called after committing the sync results.
	OnFinishAfterCommit(summaryHeight uint64) error
}

// CodeFetcher is a minimal interface for accepting discovered code hashes
// and signaling when no more code hashes will be produced from the account trie.
// Implemented by [codeSyncer] to support decoupled wiring.
type CodeFetcher interface {
	AddCode(codeHashes []common.Hash) error
	FinishCodeCollection()
	Ready() <-chan struct{}
}

// CodeSyncer is implemented by the concrete code syncer and combines
// the code fetcher and syncer behaviours so callers can use a single type.
type CodeSyncer interface {
	CodeFetcher
	Syncer
}
