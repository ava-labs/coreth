// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"golang.org/x/sync/errgroup"
)

var stateSyncSummaryKey = []byte("stateSyncSummary")

// stateSyncClientConfig defines the options and dependencies needed to construct a StateSyncerClient
type stateSyncClientConfig struct {
	enabled    bool
	skipResume bool
	// Specifies the number of blocks behind the latest state summary that the chain must be
	// in order to prefer performing state sync over falling back to the normal bootstrapping
	// algorithm.
	stateSyncMinBlocks   uint64
	stateSyncRequestSize uint16 // number of key/value pairs to ask peers for per request

	lastAcceptedHeight uint64

	chain           *eth.Ethereum
	state           *chain.State
	chaindb         ethdb.Database
	metadataDB      database.Database
	acceptedBlockDB database.Database
	db              *versiondb.Database
	atomicBackend   AtomicBackend

	client syncclient.Client

	toEngine chan<- commonEng.Message
}

type stateSyncerClient struct {
	*stateSyncClientConfig

	resumableSummary message.SyncSummary

	syncers []Syncer[*message.SyncSummary]
	cancel  context.CancelFunc
	eg      *errgroup.Group

	// State Sync results
	syncSummary  message.SyncSummary
	stateSyncErr error
}

func NewStateSyncClient(config *stateSyncClientConfig) StateSyncClient {
	return &stateSyncerClient{
		stateSyncClientConfig: config,
	}
}

type StateSyncClient interface {
	// methods that implement the client side of [block.StateSyncableVM]
	StateSyncEnabled(context.Context) (bool, error)
	GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error)
	ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error)

	// additional methods required by the evm package
	ClearOngoingSummary() error
	Shutdown() error
	Error() error
}

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer[T any] interface {
	Start(ctx context.Context, target T) error
	Wait(ctx context.Context) error
	Close() error
	UpdateSyncTarget(ctx context.Context, target T) error
}

// StateSyncEnabled returns [client.enabled], which is set in the chain's config file.
func (client *stateSyncerClient) StateSyncEnabled(context.Context) (bool, error) {
	return client.enabled, nil
}

// GetOngoingSyncStateSummary returns a state summary that was previously started
// and not finished, and sets [resumableSummary] if one was found.
// Returns [database.ErrNotFound] if no ongoing summary is found or if [client.skipResume] is true.
func (client *stateSyncerClient) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	if client.skipResume {
		return nil, database.ErrNotFound
	}

	summaryBytes, err := client.metadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := message.NewSyncSummaryFromBytes(summaryBytes, client.acceptSyncSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	client.resumableSummary = summary
	return summary, nil
}

// ClearOngoingSummary clears any marker of an ongoing state sync summary
func (client *stateSyncerClient) ClearOngoingSummary() error {
	if err := client.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return fmt.Errorf("failed to clear ongoing summary: %w", err)
	}
	if err := client.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	return nil
}

// ParseStateSummary parses [summaryBytes] to [commonEng.Summary]
func (client *stateSyncerClient) ParseStateSummary(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return message.NewSyncSummaryFromBytes(summaryBytes, client.acceptSyncSummary)
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (client *stateSyncerClient) acceptSyncSummary(proposedSummary message.SyncSummary) (block.StateSyncMode, error) {
	isResume := proposedSummary.BlockHash == client.resumableSummary.BlockHash
	if !isResume {
		// Skip syncing if the blockchain is not significantly ahead of local state,
		// since bootstrapping would be faster.
		// (Also ensures we don't sync to a height prior to local state.)
		if client.lastAcceptedHeight+client.stateSyncMinBlocks > proposedSummary.Height() {
			log.Info(
				"last accepted too close to most recent syncable block, skipping state sync",
				"lastAccepted", client.lastAcceptedHeight,
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
		<-snapshot.WipeSnapshot(client.chaindb, true)
		// Reset the snapshot generator here so that when state sync completes, snapshots will not attempt to read an
		// invalid generator.
		// Note: this must be called after WipeSnapshot is called so that we do not invalidate a partially generated snapshot.
		snapshot.ResetSnapshotGeneration(client.chaindb)
	}
	client.syncSummary = proposedSummary

	// Update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := client.metadataDB.Put(stateSyncSummaryKey, proposedSummary.Bytes()); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := client.db.Commit(); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to commit db: %w", err)
	}

	return block.StateSyncStatic, client.startSync()
}

func (client *stateSyncerClient) startSync() error {
	log.Info("Starting state sync", "summary", client.syncSummary)

	// create a cancellable ctx for the state sync goroutine
	var detachedCtx context.Context
	detachedCtx, client.cancel = context.WithCancel(context.Background())

	err := client.initSyncers()
	if err != nil {
		return fmt.Errorf("failed to initialize syncers: %w", err)
	}

	for _, syncer := range client.syncers {
		if err := syncer.Start(detachedCtx, &client.syncSummary); err != nil {
			return err
		}
	}

	var egCtx context.Context
	client.eg, egCtx = errgroup.WithContext(detachedCtx)
	for _, syncer := range client.syncers {
		client.eg.Go(func() error {
			if err := syncer.Wait(egCtx); err != nil {
				return fmt.Errorf("state syncer %T failed: %w", syncer, err)
			}
			return nil
		})
	}

	go func() {
		log.Info("Waiting for state syncer(s) to complete", "numSyncers", len(client.syncers))
		client.stateSyncErr = client.eg.Wait()
		if client.stateSyncErr != nil {
			log.Error("State sync failed", "err", err)
		} else {
			log.Info("State syncer(s) completed", "numSyncers", len(client.syncers))
			client.stateSyncErr = client.finishSync()
		}
		// client.finish(detachedCtx, err)
		log.Info("State sync complete", "err", client.stateSyncErr)

		// On error, we still can bootstrap from genesis
		client.toEngine <- commonEng.StateSyncDone
	}()

	return nil
}

// initSyncers initializes the syncers for the state sync process
func (client *stateSyncerClient) initSyncers() error {
	blockSyncer, err := statesync.NewBlockSyncer(&statesync.BlockSyncerConfig{
		Client:  client.client,
		ChainDB: client.chaindb,
	})
	if err != nil {
		return fmt.Errorf("failed to create block syncer: %w", err)
	}

	evmSyncer, err := statesync.NewStateSyncer(&statesync.StateSyncerConfig{
		Client:                   client.client,
		BatchSize:                ethdb.IdealBatchSize,
		DB:                       client.chaindb,
		MaxOutstandingCodeHashes: statesync.DefaultMaxOutstandingCodeHashes,
		NumCodeFetchingWorkers:   statesync.DefaultNumCodeFetchingWorkers,
		RequestSize:              client.stateSyncRequestSize,
	})
	if err != nil {
		return fmt.Errorf("failed to create EVM syncer: %w", err)
	}

	atomicSyncer, err := newAtomicSyncer(
		client.client,
		client.db,
		client.atomicBackend.AtomicTrie(),
		client.stateSyncRequestSize,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic syncer: %w", err)
	}
	client.syncers = []Syncer[*message.SyncSummary]{
		blockSyncer,
		evmSyncer,
		atomicSyncer,
	}
	return nil
}

// Updates dynamic syncing targets. Should never be called yet.
func (c *stateSyncerClient) UpdateSyncTarget(ctx context.Context, target Block) error {
	// TODO: remove critical log when this is fully implemented
	log.Crit("state syncer client should not be called to update sync target", "id", target.ID(), "height", target.Height())
	newSyncSummary, err := message.NewSyncSummary(
		target.ethBlock.Hash(),
		target.Height(),
		target.ethBlock.Root(),
		common.Hash{}, // won't be used
	)
	if err != nil {
		return fmt.Errorf("failed to create new sync summary: %w", err)
	}

	log.Info("Updating state sync target", "ID", target.ID(), "height", target.Height())
	for _, syncer := range c.syncers {
		if err := syncer.UpdateSyncTarget(ctx, &newSyncSummary); err != nil {
			return err
		}
	}
	return nil
}

func (client *stateSyncerClient) Shutdown() error {
	if client.cancel != nil {
		client.cancel()
	}
	if client.eg != nil {
		client.eg.Wait() // wait for the background goroutine to exit
	}
	return nil
}

func (client *stateSyncerClient) getEVMBlockFromHash(hash common.Hash) (*Block, error) {
	stateBlock, err := client.state.GetBlock(context.TODO(), ids.ID(client.syncSummary.BlockHash))
	if err != nil {
		return nil, fmt.Errorf("could not get block by hash from client state: %s", client.syncSummary.BlockHash)
	}

	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return nil, fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
	}
	evmBlock, ok := wrapper.Block.(*Block)
	if !ok {
		return nil, fmt.Errorf("could not convert block(%T) to evm.Block", stateBlock)
	}
	return evmBlock, nil
}

// finishSync is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (client *stateSyncerClient) finishSync() error {
	evmBlock, err := client.getEVMBlockFromHash(client.syncSummary.BlockHash)
	if err != nil {
		return fmt.Errorf("could not get block from client state: %w", err)
	}
	block := evmBlock.ethBlock

	if block.Hash() != client.syncSummary.BlockHash {
		return fmt.Errorf("attempted to set last summary block to unexpected block hash: (%s != %s)", block.Hash(), client.syncSummary.BlockHash)
	}
	if block.NumberU64() != client.syncSummary.BlockNumber {
		return fmt.Errorf("attempted to set last summary block to unexpected block number: (%d != %d)", block.NumberU64(), client.syncSummary.BlockNumber)
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
	client.chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err := client.chain.BlockChain().ResetToStateSyncedBlock(block); err != nil {
		return err
	}

	if err := client.updateVMMarkers(); err != nil {
		return fmt.Errorf("error updating vm markers, height=%d, hash=%s, err=%w", block.NumberU64(), block.Hash(), err)
	}

	if err := client.state.SetLastAcceptedBlock(evmBlock); err != nil {
		return err
	}

	// the chain state is already restored, and from this point on
	// the block synced to is the accepted block. the last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	if err := client.atomicBackend.ApplyToSharedMemory(block.NumberU64()); err != nil {
		return fmt.Errorf("error applying atomic trie to shared memory, height=%d, hash=%s, err=%w", block.NumberU64(), block.Hash(), err)
	}
	return nil
}

// updateVMMarkers updates the following markers in the VM's database
// and commits them atomically:
// - updates atomic trie so it will have necessary metadata for the last committed root
// - updates atomic trie so it will resume applying operations to shared memory on initialize
// - updates lastAcceptedKey
// - removes state sync progress markers
func (client *stateSyncerClient) updateVMMarkers() error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block to [vm.syncSummary] when ApplyToSharedMemory
	// is called.
	if err := client.atomicBackend.MarkApplyToSharedMemoryCursor(client.lastAcceptedHeight); err != nil {
		return err
	}
	client.atomicBackend.SetLastAccepted(client.syncSummary.BlockHash)
	if err := client.acceptedBlockDB.Put(lastAcceptedKey, client.syncSummary.BlockHash[:]); err != nil {
		return err
	}
	if err := client.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return client.db.Commit()
}

// Error returns a non-nil error if one occurred during the sync.
func (client *stateSyncerClient) Error() error { return client.stateSyncErr }
