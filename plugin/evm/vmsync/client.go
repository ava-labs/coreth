// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/blocksync"
	"github.com/ava-labs/coreth/sync/statesync"

	syncpkg "github.com/ava-labs/coreth/sync"
	syncclient "github.com/ava-labs/coreth/sync/client"
)

// BlocksToFetch is the number of the block parents the state syncs to.
// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
const BlocksToFetch = 256

var stateSyncSummaryKey = []byte("stateSyncSummary")

// BlockAcceptor provides a mechanism to update the last accepted block ID during state synchronization.
// This interface is used by the state sync process to ensure the blockchain state
// is properly updated when new blocks are synchronized from the network.
type BlockAcceptor interface {
	PutLastAcceptedID(ids.ID) error
}

// EthBlockWrapper can be implemented by a concrete block wrapper type to
// return *types.Block, which is needed to update chain pointers at the
// end of the sync operation. It also provides Accept/Reject/Verify operations
// for deferred processing during dynamic state sync.
type EthBlockWrapper interface {
	GetEthBlock() *types.Block
	Accept(context.Context) error
	Reject(context.Context) error
	Verify(context.Context) error
}

type ClientConfig struct {
	Chain      *eth.Ethereum
	State      *chain.State
	ChainDB    ethdb.Database
	Acceptor   BlockAcceptor
	VerDB      *versiondb.Database
	MetadataDB database.Database

	// Extension points.
	Parser message.SyncableParser

	// Extender is an optional extension point for the state sync process, and can be nil.
	Extender      syncpkg.Extender
	Client        syncclient.Client
	StateSyncDone chan struct{}

	// Specifies the number of blocks behind the latest state summary that the chain must be
	// in order to prefer performing state sync over falling back to the normal bootstrapping
	// algorithm.
	MinBlocks          uint64
	LastAcceptedHeight uint64
	RequestSize        uint16 // number of key/value pairs to ask peers for per request
	Enabled            bool
	SkipResume         bool
	// DynamicStateSyncEnabled toggles dynamic vs static state sync orchestration.
	DynamicStateSyncEnabled bool

	// PivotInterval advances the sync target every N blocks.
	PivotInterval uint64
}

type client struct {
	*ClientConfig

	resumableSummary message.Syncable

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State Sync results
	summary       message.Syncable
	err           error
	stateSyncOnce sync.Once

	// dynamic coordinator (only set in dynamic mode)
	coordinator *Coordinator
}

func NewClient(config *ClientConfig) Client {
	return &client{
		ClientConfig: config,
	}
}

type Client interface {
	// Methods that implement the client side of [block.StateSyncableVM].
	StateSyncEnabled(context.Context) (bool, error)
	GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error)
	ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error)

	// Additional methods required by the evm package.
	ClearOngoingSummary() error
	Shutdown() error
	Error() error
	// OnEngineAccept should be called by the engine when a block is accepted.
	// Returns true if the block was enqueued for deferred processing, false otherwise.
	OnEngineAccept(EthBlockWrapper) (bool, error)
	// OnEngineReject should be called by the engine when a block is rejected.
	// Returns true if the block was enqueued for deferred processing, false otherwise.
	OnEngineReject(EthBlockWrapper) (bool, error)
	// OnEngineVerify should be called by the engine when a block is verified.
	// Returns true if the block was enqueued for deferred processing, false otherwise.
	OnEngineVerify(EthBlockWrapper) (bool, error)
}

// StateSyncEnabled returns [client.enabled], which is set in the chain's config file.
func (c *client) StateSyncEnabled(context.Context) (bool, error) {
	return c.Enabled, nil
}

// GetOngoingSyncStateSummary returns a state summary that was previously started
// and not finished, and sets [resumableSummary] if one was found.
// Returns [database.ErrNotFound] if no ongoing summary is found or if [client.skipResume] is true.
func (c *client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	if c.SkipResume {
		return nil, database.ErrNotFound
	}

	summaryBytes, err := c.MetadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := c.Parser.Parse(summaryBytes, c.acceptSyncSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	c.resumableSummary = summary
	return summary, nil
}

// ClearOngoingSummary clears any marker of an ongoing state sync summary
func (c *client) ClearOngoingSummary() error {
	if err := c.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return fmt.Errorf("failed to clear ongoing summary: %w", err)
	}
	if err := c.VerDB.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	return nil
}

// ParseStateSummary parses [summaryBytes] to [commonEng.Summary]
func (c *client) ParseStateSummary(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return c.Parser.Parse(summaryBytes, c.acceptSyncSummary)
}

// OnEngineAccept should be invoked by the engine when a block is accepted.
// It best-effort enqueues the block for post-finalization execution and
// advances the coordinator's sync target using the block's hash/root/height.
// Returns true if the block was enqueued for deferred processing, false otherwise.
// During batch execution, only enqueuing occurs to prevent recursion.
func (c *client) OnEngineAccept(b EthBlockWrapper) (bool, error) {
	// Skip sync target update during batch execution to prevent recursion.
	// Blocks enqueued during batch execution will be processed in the next batch.
	if c.coordinator.CurrentState() == StateExecutingBatch {
		// Still enqueue the block for processing in the next batch.
		ethb := c.enqueueBlockOperation(b, OpAccept)
		return ethb != nil, nil
	}

	// Enqueue the block first.
	ethb := c.enqueueBlockOperation(b, OpAccept)
	if ethb == nil {
		return false, nil
	}

	// Only update sync target on accept operations when not in batch execution.
	// If UpdateSyncTarget fails, the block is already enqueued, which is acceptable
	// as it will be processed later. The error is returned to allow the caller to
	// handle it appropriately (e.g., log a warning).
	syncTarget := newSyncTarget(ethb.Hash(), ethb.Root(), ethb.NumberU64())
	if err := c.coordinator.UpdateSyncTarget(syncTarget); err != nil {
		// Block is enqueued but sync target update failed. Return error to allow
		// caller to handle it, but indicate block was enqueued.
		return true, fmt.Errorf("block enqueued but sync target update failed: %w", err)
	}
	return true, nil
}

// OnEngineReject should be invoked by the engine when a block is rejected.
// It best-effort enqueues the block for post-finalization execution.
// Returns true if the block was enqueued for deferred processing, false otherwise.
func (c *client) OnEngineReject(b EthBlockWrapper) (bool, error) {
	ethb := c.enqueueBlockOperation(b, OpReject)
	return ethb != nil, nil
}

// OnEngineVerify should be invoked by the engine when a block is verified.
// It best-effort enqueues the block for post-finalization execution.
// Returns true if the block was enqueued for deferred processing, false otherwise.
func (c *client) OnEngineVerify(b EthBlockWrapper) (bool, error) {
	ethb := c.enqueueBlockOperation(b, OpVerify)
	return ethb != nil, nil
}

func (c *client) Shutdown() error {
	// Unify completion - cancel context, set terminal outcome, and close [ClientConfig.StateSyncDone] once.
	c.signalDone(context.Canceled)
	c.wg.Wait()
	return nil
}

// Error returns a non-nil error if one occurred during the sync.
func (c *client) Error() error {
	return c.err
}

func (c *client) registerSyncers(registry *SyncerRegistry) error {
	// Register block syncer.
	blockSyncer, err := c.createBlockSyncer(c.summary.GetBlockHash(), c.summary.Height())
	if err != nil {
		return fmt.Errorf("failed to create block syncer: %w", err)
	}

	codeQueue, err := c.createCodeQueue()
	if err != nil {
		return fmt.Errorf("failed to create code queue: %w", err)
	}

	codeSyncer, err := c.createCodeSyncer(codeQueue.CodeHashes())
	if err != nil {
		return fmt.Errorf("failed to create code syncer: %w", err)
	}

	stateSyncer, err := c.createEVMSyncer(codeQueue)
	if err != nil {
		return fmt.Errorf("failed to create EVM state syncer: %w", err)
	}

	var atomicSyncer syncpkg.Syncer
	if c.Extender != nil {
		atomicSyncer, err = c.createAtomicSyncer()
		if err != nil {
			return fmt.Errorf("failed to create atomic syncer: %w", err)
		}
	}

	syncers := []syncpkg.Syncer{
		blockSyncer,
		codeSyncer,
		stateSyncer,
	}
	if atomicSyncer != nil {
		syncers = append(syncers, atomicSyncer)
	}

	for _, s := range syncers {
		if err := registry.Register(s); err != nil {
			return fmt.Errorf("failed to register %s syncer: %w", s.Name(), err)
		}
	}

	return nil
}

func (c *client) createBlockSyncer(fromHash common.Hash, fromHeight uint64) (syncpkg.Syncer, error) {
	return blocksync.NewSyncer(
		c.Client,
		c.ChainDB,
		fromHash,
		fromHeight,
		BlocksToFetch,
	)
}

func (c *client) createEVMSyncer(queue *statesync.CodeQueue) (syncpkg.Syncer, error) {
	return statesync.NewSyncer(
		c.Client,
		c.ChainDB,
		c.summary.GetBlockRoot(),
		queue,
		c.RequestSize,
	)
}

func (c *client) createCodeQueue() (*statesync.CodeQueue, error) {
	return statesync.NewCodeQueue(
		c.ChainDB,
		c.StateSyncDone,
	)
}

func (c *client) createCodeSyncer(codeHashes <-chan common.Hash) (syncpkg.Syncer, error) {
	return statesync.NewCodeSyncer(c.Client, c.ChainDB, codeHashes)
}

func (c *client) createAtomicSyncer() (syncpkg.Syncer, error) {
	return c.Extender.CreateSyncer(c.Client, c.VerDB, c.summary)
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (c *client) acceptSyncSummary(proposedSummary message.Syncable) (block.StateSyncMode, error) {
	// If dynamic sync is already running, treat new summaries as target updates.
	// Pivot gating is enforced inside the coordinator's policy.
	if c.DynamicStateSyncEnabled && c.coordinator != nil {
		if c.coordinator.CurrentState() == StateRunning {
			if err := c.coordinator.UpdateSyncTarget(proposedSummary); err != nil {
				return block.StateSyncSkipped, err
			}
			return block.StateSyncDynamic, nil
		}
	}

	isResume := c.resumableSummary != nil &&
		proposedSummary.GetBlockHash() == c.resumableSummary.GetBlockHash()
	if !isResume {
		// Skip syncing if the blockchain is not significantly ahead of local state,
		// since bootstrapping would be faster.
		// (Also ensures we don't sync to a height prior to local state.)
		if c.LastAcceptedHeight+c.MinBlocks > proposedSummary.Height() {
			log.Info(
				"last accepted too close to most recent syncable block, skipping state sync",
				"lastAccepted", c.LastAcceptedHeight,
				"syncableHeight", proposedSummary.Height(),
			)
			return block.StateSyncSkipped, nil
		}

		// Wipe the snapshot completely if we are not resuming from an existing sync, so that we do not
		// use a corrupted snapshot.
		// Note: this assumes that when the node is started with state sync disabled, the in-progress state
		// sync marker will be wiped, so we do not accidentally resume progress from an incorrect version
		// of the snapshot. (if switching between versions that come before this change and back this could
		// lead to the snapshot not being cleaned up correctly)
		<-snapshot.WipeSnapshot(c.ChainDB, true)
		// Reset the snapshot generator here so that when state sync completes, snapshots will not attempt to read an
		// invalid generator.
		// Note: this must be called after WipeSnapshot is called so that we do not invalidate a partially generated snapshot.
		snapshot.ResetSnapshotGeneration(c.ChainDB)
	}
	c.summary = proposedSummary

	// Update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := c.MetadataDB.Put(stateSyncSummaryKey, proposedSummary.Bytes()); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := c.VerDB.Commit(); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to commit db: %w", err)
	}

	log.Info("Starting state sync", "summary", proposedSummary.GetBlockHash().Hex())
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	registry := NewSyncerRegistry()
	if err := c.registerSyncers(registry); err != nil {
		return block.StateSyncSkipped, err
	}

	if c.DynamicStateSyncEnabled {
		return c.stateSyncDynamic(ctx, registry), nil
	}

	return c.stateSyncStatic(ctx, registry), nil
}

// stateSyncStatic launches the static state sync in a goroutine and returns the mode.
func (c *client) stateSyncStatic(ctx context.Context, registry *SyncerRegistry) block.StateSyncMode {
	c.wg.Add(1)
	go func() {
		var err error

		defer func() {
			c.wg.Done()
			c.signalDone(err)
		}()

		if err = registry.RunSyncerTasks(ctx, c.summary); err != nil {
			log.Error("static state sync completed with error, notifying engine", "err", err)
			return
		}

		if err = registry.FinalizeAll(ctx); err != nil {
			log.Error("finalizing syncers failed after static state sync", "err", err)
			return
		}

		if err = c.finishSync(ctx); err != nil {
			log.Error("finalizing VM markers failed after static state sync", "err", err)
			return
		}

		log.Info("static state sync completed, notifying engine")
	}()
	log.Info("static state sync started", "summary", c.summary)
	return block.StateSyncStatic
}

// stateSyncDynamic launches the dynamic state sync in a goroutine and returns the mode.
func (c *client) stateSyncDynamic(ctx context.Context, registry *SyncerRegistry) block.StateSyncMode {
	c.coordinator = NewCoordinator(
		registry,
		Callbacks{
			FinalizeVM: func(ctx context.Context, _ message.Syncable) error {
				return c.finishSync(ctx)
			},
			OnDone: func(err error) {
				c.signalDone(err)
				if err != nil {
					log.Error("dynamic state sync completed with error, notifying engine", "err", err)
				} else {
					log.Info("dynamic state sync completed successfully, notifying engine")
				}
			},
		},
		WithPivotInterval(c.PivotInterval),
	)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.coordinator.Start(ctx, c.summary)
	}()
	log.Info("dynamic state sync started", "summary", c.summary)
	return block.StateSyncDynamic
}

// enqueueBlockOperation handles the common logic for enqueuing block operations.
// Returns the eth block if the operation was enqueued successfully, nil otherwise.
// The returned block can be used to avoid duplicate GetEthBlock() calls.
func (c *client) enqueueBlockOperation(b EthBlockWrapper, op BlockOperationType) *types.Block {
	if !c.DynamicStateSyncEnabled || c.coordinator == nil {
		return nil
	}

	ethb := b.GetEthBlock()
	// Best-effort enqueue, allowed during Running or StateExecutingBatch.
	ok := c.coordinator.AddBlockOperation(b, op)
	if !ok && ethb != nil {
		log.Warn("could not enqueue block operation for post-finalization execution", "hash", ethb.Hash(), "block_operation", op.String(), "height", ethb.NumberU64())
		return nil
	}

	// Return the eth block for callers that need it (e.g., OnEngineAccept).
	return ethb
}

// finishSync is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (c *client) finishSync(ctx context.Context) error {
	stateBlock, err := c.State.GetBlock(ctx, ids.ID(c.summary.GetBlockHash()))
	if err != nil {
		return fmt.Errorf("could not get block by hash from client state: %s", c.summary.GetBlockHash())
	}

	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
	}
	wrappedBlock := wrapper.Block

	evmBlockGetter, ok := wrappedBlock.(EthBlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to evm.EthBlockWrapper", stateBlock)
	}

	block := evmBlockGetter.GetEthBlock()

	if block.Hash() != c.summary.GetBlockHash() {
		return fmt.Errorf("attempted to set last summary block to unexpected block hash: (%s != %s)", block.Hash(), c.summary.GetBlockHash())
	}
	if block.NumberU64() != c.summary.Height() {
		return fmt.Errorf("attempted to set last summary block to unexpected block number: (%d != %d)", block.NumberU64(), c.summary.Height())
	}

	// BloomIndexer needs to know that some parts of the chain are not available
	// and cannot be indexed. This is done by calling [AddCheckpoint] here.
	// Since the indexer uses sections of size [params.BloomBitsBlocks] (= 4096),
	// each block is indexed in section number [blockNumber/params.BloomBitsBlocks].
	// To allow the indexer to start with the block we just synced to,
	// we create a checkpoint for its parent.
	// Note: This requires assuming the synced block height is divisible
	// by [params.BloomBitsBlocks].
	parentHeight := block.NumberU64() - 1
	parentHash := block.ParentHash()
	c.Chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	// Check for cancellation before starting finalization operations.
	// These operations are sequential and not cancellable mid-execution, so a
	// single check at the beginning is sufficient.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("finishSync cancelled before finalization: %w", err)
	}

	// Execute operations sequentially. These are critical finalization operations
	// that update VM state and should complete atomically.
	if err := c.Chain.BlockChain().ResetToStateSyncedBlock(block); err != nil {
		return err
	}

	if c.Extender != nil {
		if err := c.Extender.OnFinishBeforeCommit(c.LastAcceptedHeight, c.summary); err != nil {
			return err
		}
	}

	if err := c.commitVMMarkers(); err != nil {
		return fmt.Errorf("error updating vm markers, height=%d, hash=%s, err=%w", block.NumberU64(), block.Hash(), err)
	}

	if err := c.State.SetLastAcceptedBlock(wrappedBlock); err != nil {
		return err
	}

	if c.Extender != nil {
		if err := c.Extender.OnFinishAfterCommit(block.NumberU64()); err != nil {
			return err
		}
	}

	return nil
}

// commitVMMarkers updates the following markers in the VM's database
// and commits them atomically:
// - updates atomic trie so it will have necessary metadata for the last committed root
// - updates atomic trie so it will resume applying operations to shared memory on initialize
// - updates lastAcceptedKey
// - removes state sync progress markers
func (c *client) commitVMMarkers() error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block to [vm.syncSummary] when ApplyToSharedMemory
	// is called.
	id := ids.ID(c.summary.GetBlockHash())
	if err := c.Acceptor.PutLastAcceptedID(id); err != nil {
		return err
	}
	if err := c.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return c.VerDB.Commit()
}

// signalDone sets the terminal error exactly once, signals completion to the engine.
func (c *client) signalDone(err error) {
	c.stateSyncOnce.Do(func() {
		c.err = err
		if c.cancel != nil {
			c.cancel()
		}
		close(c.StateSyncDone)
	})
}
