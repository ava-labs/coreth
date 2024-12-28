// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
// TODO: move to separate package
package atomic

import (
	"context"
	"fmt"

	syncclient "github.com/ava-labs/coreth/sync/client"

	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ethereum/go-ethereum/log"
)

type atomicSyncExtender struct {
	backend              AtomicBackend
	stateSyncRequestSize uint16
}

func NewAtomicSyncExtender(backend AtomicBackend, stateSyncRequestSize uint16) sync.Extender {
	return &atomicSyncExtender{
		backend:              backend,
		stateSyncRequestSize: stateSyncRequestSize,
	}
}

func (a *atomicSyncExtender) Sync(ctx context.Context, client syncclient.Client, syncSummary message.Syncable) error {
	atomicSyncSummary, ok := syncSummary.(*AtomicBlockSyncSummary)
	if !ok {
		return fmt.Errorf("expected AtomicBlockSyncSummary, got %T", syncSummary)
	}
	log.Info("atomic tx: sync starting", "root", atomicSyncSummary)
	atomicSyncer, err := a.backend.Syncer(client, atomicSyncSummary.AtomicRoot, atomicSyncSummary.BlockNumber, a.stateSyncRequestSize)
	if err != nil {
		return err
	}
	if err := atomicSyncer.Start(ctx); err != nil {
		return err
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
		return err
	}
	a.backend.SetLastAccepted(syncSummary.GetBlockHash())
	return nil
}

func (a *atomicSyncExtender) OnFinishAfterCommit(summaryHeight uint64) error {
	// the chain state is already restored, and from this point on
	// the block synced to is the accepted block. the last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	return a.backend.ApplyToSharedMemory(summaryHeight)
}
