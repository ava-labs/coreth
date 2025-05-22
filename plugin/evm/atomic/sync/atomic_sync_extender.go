// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	syncclient "github.com/ava-labs/coreth/sync/client"

	"github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"

	"github.com/ava-labs/libevm/log"
)

var _ sync.Extender = (*atomicSyncExtender)(nil)

type atomicSyncExtender struct {
	backend              *state.AtomicBackend
	atomicTrie           *state.AtomicTrie
	stateSyncRequestSize uint16
}

// Initialize initializes the atomic sync extender with the atomic backend and atomic trie.
func NewAtomicSyncExtender(backend *state.AtomicBackend, atomicTrie *state.AtomicTrie, stateSyncRequestSize uint16) *atomicSyncExtender {
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
	atomicSyncer, err := newAtomicSyncer(
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
