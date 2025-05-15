// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	syncclient "github.com/ava-labs/coreth/sync/client"

	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
)

var _ sync.Extender = (*atomicSyncExtender)(nil)

type AtomicBackend interface {
	// ApplyToSharedMemory applies the atomic operations that have been indexed into the trie
	// but not yet applied to shared memory for heights less than or equal to [lastAcceptedBlock].
	// This executes operations in the range [cursorHeight+1, lastAcceptedBlock].
	// The cursor is initially set by  MarkApplyToSharedMemoryCursor to signal to the atomic trie
	// the range of operations that were added to the trie without being executed on shared memory.
	ApplyToSharedMemory(lastAcceptedBlock uint64) error

	// MarkApplyToSharedMemoryCursor marks the atomic trie as containing atomic ops that
	// have not been executed on shared memory starting at [previousLastAcceptedHeight+1].
	// This is used when state sync syncs the atomic trie, such that the atomic operations
	// from [previousLastAcceptedHeight+1] to the [lastAcceptedHeight] set by state sync
	// will not have been executed on shared memory.
	MarkApplyToSharedMemoryCursor(previousLastAcceptedHeight uint64) error

	// SetLastAccepted is used after state-sync to reset the last accepted block.
	SetLastAccepted(lastAcceptedHash common.Hash)
}

type atomicSyncExtender struct {
	backend              AtomicBackend
	atomicTrie           AtomicTrie
	stateSyncRequestSize uint16
}

// Initialize initializes the atomic sync extender with the atomic backend and atomic trie.
func NewAtomicSyncExtender(backend AtomicBackend, atomicTrie AtomicTrie, stateSyncRequestSize uint16) *atomicSyncExtender {
	return &atomicSyncExtender{
		backend:              backend,
		atomicTrie:           atomicTrie,
		stateSyncRequestSize: stateSyncRequestSize,
	}
}

func (a *atomicSyncExtender) Sync(ctx context.Context, client syncclient.LeafClient, verDB *versiondb.Database, syncSummary message.Syncable) error {
	atomicSyncSummary, ok := syncSummary.(*AtomicSyncSummary)
	if !ok {
		return fmt.Errorf("expected *AtomicBlockSyncSummary, got %T", syncSummary)
	}
	log.Info("atomic tx: sync starting", "root", atomicSyncSummary)
	atomicSyncer, err := NewAtomicSyncer(
		client,
		verDB,
		a.atomicTrie,
		atomicSyncSummary.AtomicRoot,
		atomicSyncSummary.BlockNumber,
		a.stateSyncRequestSize,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic syncer: %w", err)
	}
	if err := atomicSyncer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start atomic syncer: %w", err)
	}
	err = <-atomicSyncer.Done()
	log.Info("atomic tx: sync finished", "root", atomicSyncSummary.AtomicRoot, "err", err)
	return err
}

func (a *atomicSyncExtender) OnFinishBeforeCommit(lastAcceptedHeight uint64, syncSummary message.Syncable) error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block when ApplyToSharedMemory
	// is called.
	if err := a.backend.MarkApplyToSharedMemoryCursor(lastAcceptedHeight); err != nil {
		return fmt.Errorf("failed to mark apply to shared memory cursor before commit: %w", err)
	}
	a.backend.SetLastAccepted(syncSummary.GetBlockHash())
	return nil
}

func (a *atomicSyncExtender) OnFinishAfterCommit(summaryHeight uint64) error {
	// the chain state is already restored, and, from this point on,
	// the block synced to is the accepted block. The last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and, even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	if err := a.backend.ApplyToSharedMemory(summaryHeight); err != nil {
		return fmt.Errorf("failed to apply atomic trie to shared memory after commit: %w", err)
	}
	return nil
}
