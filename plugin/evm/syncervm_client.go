// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	ethstatesync "github.com/ava-labs/coreth/plugin/evm/statesync"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

var (
	_ EVMSyncer = &ethstatesync.DynamicSyncer{}
)

const (
	// State sync fetches [parentsToGet] parents of the block it syncs to.
	// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
	parentsToGet = 256
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

	///
	useUpstream    bool
	network        peer.Network
	appSender      commonEng.AppSender
	stateSyncNodes []ids.NodeID
	scheme         string
}

type stateSyncerClient struct {
	*stateSyncClientConfig

	resumableSummary message.SyncSummary

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State Sync results
	syncSummary  message.SyncSummary
	stateSyncErr error

	// Dynamic sync
	syncing   utils.Atomic[bool]
	syncQueue *SyncQueue
}

func NewStateSyncClient(config *stateSyncClientConfig) StateSyncClient {
	return &stateSyncerClient{
		stateSyncClientConfig: config,
		syncQueue:             NewSyncQueue(),
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

	// Methods to enable dynamic state sync
	GetSyncQueue() *SyncQueue
}

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
type Syncer interface {
	Start(ctx context.Context) error
	Done() <-chan error
}

// EVMSyncer represents a Syncer that
// has a block hash that can be used to identify the
// eth block that was synced to.
type EVMSyncer interface {
	Syncer
	Hash() common.Hash
}

// GetSyncQueue returns the client sync queue.
// May return nil.
func (client *stateSyncerClient) GetSyncQueue() *SyncQueue {
	return client.syncQueue
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

// stateSync blockingly performs the state sync for the EVM state and the atomic state
// to [client.syncSummary]. returns an error if one occurred.
func (client *stateSyncerClient) stateSync(ctx context.Context) error {
	client.syncing.Set(true)
	defer client.syncing.Set(false)
	if err := client.syncBlocks(ctx, client.syncSummary.BlockHash, client.syncSummary.BlockNumber, parentsToGet); err != nil {
		return err
	}

	// In the dynamic case, sync atomic trie first. Otherwise, sync EVM trie first.
	// Sync the EVM trie and then the atomic trie. These steps could be done
	// in parallel or in the opposite order in the static case, as long as
	// the finishing steps are done together afterward (which they are not).
	// Otherwise, stateTrie must go first
	// For dynamic, much simpler to do atomic trie first
	if err := client.syncAtomicTrie(ctx); err != nil {
		return err
	}

	// Now that the atomic trie is synced, we can unblock the state sync queue
	client.syncQueue.Unblock()

	// Sync the state trie
	if err := client.syncStateTrie(ctx); err != nil {
		return err
	}

	// Now that the state trie is synced, we can execute the callbacks in the queue
	client.syncQueue.Block()
	return client.syncQueue.ExecQueue()
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

	log.Info("Starting state sync", "summary", proposedSummary)

	// create a cancellable ctx for the state sync goroutine
	ctx, cancel := context.WithCancel(context.Background())
	client.cancel = cancel
	client.wg.Add(1) // track the state sync goroutine so we can wait for it on shutdown

	if client.useUpstream {
		// Ensures bootstrapping begins at the correct place
		evmBlock, err := client.getEVMBlockFromHash(client.syncSummary.BlockHash)
		if err != nil {
			return block.StateSyncSkipped, fmt.Errorf("could not get block by hash %s from client state: %w", client.syncSummary.BlockHash, err)
		}
		if err := client.state.SetLastAcceptedBlock(evmBlock); err != nil {
			return block.StateSyncSkipped, err
		}
	}

	// Block sync queue until the atomic trie is synced
	client.syncQueue.Block()
	client.syncQueue.SetEnabled(true)

	go func() {
		defer client.wg.Done()
		defer cancel()

		if err := client.stateSync(ctx); err != nil {
			client.stateSyncErr = err
		} else {
			client.stateSyncErr = client.finalizeStateSync()
		}
		// notify engine regardless of whether err == nil,
		// this error will be propagated to the engine when it calls
		// vm.SetState(snow.Bootstrapping)
		log.Info("stateSync completed, notifying engine", "err", client.stateSyncErr)
		client.toEngine <- commonEng.StateSyncDone
	}()

	if client.useUpstream {
		return block.StateSyncDynamic, nil
	}
	return block.StateSyncStatic, nil
}

// syncBlocks fetches (up to) [parentsToGet] blocks from peers
// using [client] and writes them to disk.
// the process begins with [fromHash] and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
func (client *stateSyncerClient) syncBlocks(ctx context.Context, fromHash common.Hash, fromHeight uint64, parentsToGet int) error {
	nextHash := fromHash
	nextHeight := fromHeight
	parentsPerRequest := uint16(32)

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for parentsToGet >= 0 {
		blk := rawdb.ReadBlock(client.chaindb, nextHash, nextHeight)
		if blk != nil {
			// block exists
			nextHash = blk.ParentHash()
			nextHeight--
			parentsToGet--
			continue
		}

		// block was not found
		break
	}

	// get any blocks we couldn't find on disk from peers and write
	// them to disk.
	batch := client.chaindb.NewBatch()
	for i := parentsToGet - 1; i >= 0 && (nextHash != common.Hash{}); {
		if err := ctx.Err(); err != nil {
			return err
		}
		blocks, err := client.client.GetBlocks(ctx, nextHash, nextHeight, parentsPerRequest)
		if err != nil {
			log.Error("could not get blocks from peer", "err", err, "nextHash", nextHash, "remaining", i+1)
			return err
		}
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

			i--
			nextHash = block.ParentHash()
			nextHeight--
		}
		log.Info("fetching blocks from peer", "remaining", i+1, "total", parentsToGet)
	}
	log.Info("fetched blocks from peer", "total", parentsToGet)
	return batch.Write()
}

func (client *stateSyncerClient) syncAtomicTrie(ctx context.Context) error {
	log.Info("atomic tx: sync starting", "root", client.syncSummary.AtomicRoot)
	atomicSyncer, err := newAtomicSyncer(
		client.client,
		client.db,
		client.atomicBackend.AtomicTrie(),
		client.syncSummary.AtomicRoot,
		client.syncSummary.BlockNumber,
		client.stateSyncRequestSize,
	)
	if err != nil {
		return err
	}
	if err := atomicSyncer.Start(ctx); err != nil {
		return err
	}
	err = <-atomicSyncer.Done()
	log.Info("atomic tx: sync finished", "root", client.syncSummary.AtomicRoot, "err", err)
	return client.finishAtomicSync(client.syncSummary.BlockHash, client.syncSummary.BlockNumber)
}

func (client *stateSyncerClient) syncStateTrie(ctx context.Context) error {
	log.Info("state sync: sync starting", "root", client.syncSummary.BlockRoot)

	var (
		evmSyncer EVMSyncer
		err       error
	)
	if client.useUpstream {
		log.Warn("Using upstream state syncer (untested)")
		evmBlock, err := client.getEVMBlockFromHash(client.syncSummary.BlockHash)
		if err != nil {
			return fmt.Errorf("Could not get evmBlock from hash during acceptSyncSummary: %s, err: %w", client.syncSummary.BlockHash, err)
		}
		evmSyncer, err = ethstatesync.NewDynamicSyncer(&ethstatesync.DynamicSyncConfig{
			ChainDB:         client.chaindb,
			Scheme:          client.scheme,
			FirstPivotBlock: evmBlock.ethBlock,
			StateSyncNodes:  client.stateSyncNodes,
			Network:         client.network,
			PivotChan:       client.syncQueue.ReceivePivot(),
		})

		if err != nil {
			return fmt.Errorf("Could not create dynamic syncer: %w", err)
		}

		log.Info("Set LastAcceptedBlock as first pivot", "id", evmBlock.ID(), "height", evmBlock.Height(), "timestamp", evmBlock.Timestamp())
	} else {
		evmSyncer, err = statesync.NewStateSyncer(&statesync.StateSyncerConfig{
			Client:                   client.client,
			Root:                     client.syncSummary.BlockRoot,
			Hash:                     client.syncSummary.BlockHash,
			BatchSize:                ethdb.IdealBatchSize,
			DB:                       client.chaindb,
			MaxOutstandingCodeHashes: statesync.DefaultMaxOutstandingCodeHashes,
			NumCodeFetchingWorkers:   statesync.DefaultNumCodeFetchingWorkers,
			RequestSize:              client.stateSyncRequestSize,
		})
		if err != nil {
			return err
		}
	}
	if err := evmSyncer.Start(ctx); err != nil {
		return err
	}
	err = <-evmSyncer.Done()
	if err != nil {
		return fmt.Errorf("state sync failed: %w", err)
	}
	return client.finishStateSync(evmSyncer.Hash())
}

func (client *stateSyncerClient) Shutdown() error {
	if client.cancel != nil {
		client.cancel()
	}
	client.wg.Wait() // wait for the background goroutine to exit
	return nil
}

func (client *stateSyncerClient) getEVMBlockFromHash(blockHash common.Hash) (*Block, error) {
	// Must first find first pivot block to signal bootstrapper
	stateBlock, err := client.state.GetBlock(context.TODO(), ids.ID(blockHash))
	if err != nil {
		return nil, fmt.Errorf("could not get block by hash %s from client state: %w", blockHash, err)
	}
	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return nil, fmt.Errorf("could not convert block to *chain.BlockWrapper, type %T, hash %s", wrapper, blockHash)
	}
	evmBlock, ok := wrapper.Block.(*Block)
	if !ok {
		return nil, fmt.Errorf("could not convert block(%T) to evm.Block", stateBlock)
	}

	return evmBlock, nil
}

// finishSync is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (client *stateSyncerClient) finishStateSync(blockHash common.Hash) error {
	log.Debug("Called finishSync with", "hash", blockHash)
	evmBlock, err := client.getEVMBlockFromHash(blockHash)
	if err != nil {
		return fmt.Errorf("Could not get evmBlock from hash during finishStateSync: %w", err)
	}

	block := evmBlock.ethBlock
	log.Debug("Found block for finishSync", "hash", block.Hash(), "height", block.NumberU64())

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

	// Silence the bloom indexer if we dynamically synced
	client.chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err := client.chain.BlockChain().ResetToStateSyncedBlock(block); err != nil {
		return err
	}

	if err := client.state.SetLastAcceptedBlock(evmBlock); err != nil {
		return err
	}

	// Must rebuild the snapshot if snap-sync is enabled
	snaps := evmBlock.vm.blockChain.Snapshots()
	cb := evmBlock.vm.blockChain.CurrentBlock()
	if snaps == nil {
		log.Debug("No snapshots, skipping")
		return nil
	}

	// Eventually will be removed. Currently good to know for debugging purposes
	if err := snaps.Verify(cb.Root); err != nil {
		log.Info("could not verify snapshots, attempt rebuild", "err", err)
		snaps.Rebuild(cb.Hash(), cb.Root)
	}
	return nil
}

// finishAtomicSync updates the following markers in the VM's database
// and commits them atomically:
// - updates atomic trie so it will have necessary metadata for the last committed root
// - updates atomic trie so it will resume applying operations to shared memory on initialize
// - updates lastAcceptedKey
func (client *stateSyncerClient) finishAtomicSync(blockHash common.Hash, blockHeight uint64) error {
	// It's possible that the height in acceptedBlockDB > syncSummary.Height if the node
	// was restarted during state sync. In this case, we should not apply to shared memory
	lastAcceptedBytes, lastAcceptedErr := client.acceptedBlockDB.Get(lastAcceptedKey)
	if lastAcceptedErr == nil && len(lastAcceptedBytes) == common.HashLength {
		lastAcceptedHash := common.BytesToHash(lastAcceptedBytes)
		height := rawdb.ReadHeaderNumber(client.chaindb, lastAcceptedHash)
		if height != nil && *height > blockHeight {
			log.Info("Skipping ApplyToSharedMemory due to restart", "accepted height", *height, "syncHeight", blockHeight)
			return nil
		}
	}

	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block to [vm.syncSummary] when ApplyToSharedMemory
	// is called.
	if err := client.atomicBackend.MarkApplyToSharedMemoryCursor(client.lastAcceptedHeight); err != nil {
		return err
	}
	client.atomicBackend.SetLastAccepted(blockHash)
	if err := client.acceptedBlockDB.Put(lastAcceptedKey, blockHash[:]); err != nil {
		return err
	}
	if err := client.db.Commit(); err != nil {
		return err
	}

	evmBlock, err := client.getEVMBlockFromHash(blockHash)
	if err != nil {
		return fmt.Errorf("Could not get evmBlock from hash during finishAtomicSync: %w", err)
	}
	// TODO edit this comment
	// the chain state is already restored, and from this point on
	// the block synced to is the accepted block. the last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	return client.atomicBackend.ApplyToSharedMemory(evmBlock.Height())
}

func (client *stateSyncerClient) finalizeStateSync() error {
	if err := client.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	if err := client.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	// If state sync finished, we can unblock the queue
	// If there was an error, will likely immediately cause critical error
	client.syncQueue.SetEnabled(false)
	client.syncQueue.Unblock()

	// To avoid checking locks, will cleanup syncQueue
	client.syncQueue = nil
	return nil
}

// Error returns a non-nil error if one occurred during the sync.
func (client *stateSyncerClient) Error() error { return client.stateSyncErr }
