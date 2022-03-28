// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/client/stats"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// maximum number of blocks to fetch after the fast sync block
	// Note: The last 256 block hashes are necessary to support
	// the BLOCKHASH opcode.
	parentsToGet = 256

	// maximum number of retry attempts for a single state sync request
	maxRetryAttempts = uint8(32)
	// maximum delay in between successive requests
	defaultMaxRetryDelay = 10 * time.Second

	// defaultStateSyncMinBlocks is the minimum number of blocks the blockchain
	// should be ahead of local last accepted to perform state sync.
	// This constant is chosen so if normal bootstrapping would be faster than
	// state sync, normal bootstrapping is preferred.
	// time assumptions:
	// - normal bootstrap processing time: ~14 blocks / second
	// - state sync time: ~6 hrs.
	defaultStateSyncMinBlocks = 300_000

	// defaultSyncableInterval is chosen to balance the number of historical
	// trie roots nodes participating in the state sync protocol need to keep,
	// and the time nodes joining the network have to complete the sync.
	// Higher values also add to the worst case number of blocks new nodes
	// joining the network must process after state sync.
	// defaultSyncableInterval must be a multiple of [core.CommitInterval]
	// time assumptions:
	// - block issuance time ~2s (time per 4096 blocks ~2hrs).
	// - state sync time: ~6 hrs. (assuming even slow nodes will complete in ~12hrs)
	// - normal bootstrap processing time: ~14 blocks / second
	// 4 * 4096 allows a sync time of up to 16 hrs and keeping two historical
	// trie roots (in addition to the current state trie), while adding a
	// worst case overhead of ~15 mins in form of post-sync block processing
	// time for new nodes.
	defaultSyncableInterval = 4 * core.CommitInterval
)

var stateSyncSummaryKey = []byte("stateSyncSummary")

type stateSyncConfig struct {
	*vmState
	statsEnabled     bool
	enabled          bool
	codec            codec.Manager
	netCodec         codec.Manager
	network          peer.Network
	client           peer.NetworkClient
	toEngine         chan<- commonEng.Message
	stateSyncIDs     []ids.ShortID
	minBlocks        uint64
	syncableInterval uint64
}

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	Start(ctx context.Context)
	Done() <-chan error
}

type stateSyncer struct {
	*stateSyncConfig

	client syncclient.Client

	syncer           Syncer
	stateSyncError   error
	stateSyncSummary []byte
	stateSyncBlock   message.SyncableBlock

	// used to stop syncer
	cancel func()
}

func NewStateSyncer(config *stateSyncConfig) *stateSyncer {
	var syncerStats stats.ClientSyncerStats
	if config.statsEnabled {
		syncerStats = stats.NewClientSyncerStats()
	} else {
		syncerStats = stats.NewNoOpStats()
	}
	return &stateSyncer{
		stateSyncConfig: config,
		client: syncclient.NewClient(&syncclient.ClientConfig{
			NetworkClient:    config.client,
			Codec:            config.netCodec,
			Stats:            syncerStats,
			MaxAttempts:      maxRetryAttempts,
			MaxRetryDelay:    defaultMaxRetryDelay,
			StateSyncNodeIDs: config.stateSyncIDs,
		}),
	}
}

func (vm *stateSyncer) stateSummaryAtHeight(height uint64) (message.SyncableBlock, error) {
	atomicRoot, err := vm.atomicTrie.Root(height)
	if err != nil {
		return message.SyncableBlock{}, fmt.Errorf("error getting atomic trie root for height (%d): %w", height, err)
	}

	if (atomicRoot == common.Hash{}) {
		return message.SyncableBlock{}, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	blk := vm.chain.GetBlockByNumber(height)
	if blk == nil {
		return message.SyncableBlock{}, fmt.Errorf("block not found for height (%d)", height)
	}

	if !vm.chain.BlockChain().HasState(blk.Root()) {
		return message.SyncableBlock{}, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}

	response := message.SyncableBlock{
		AtomicRoot:  atomicRoot,
		BlockHash:   blk.Hash(),
		BlockRoot:   blk.Root(),
		BlockNumber: height,
	}
	log.Debug("sync roots returned", "roots", response, "height", height)
	return response, nil
}

// StateSyncEnabled returns whether the state sync is enabled on this VM
func (vm *stateSyncer) StateSyncEnabled() (bool, error) {
	return vm.enabled, nil
}

// ParseSummary implements StateSyncableVM and returns a Summary interface
// represented by [summaryBytes].
func (vm *VM) ParseSummary(summaryBytes []byte) (commonEng.Summary, error) {
	summaryBlk, err := summaryToSyncableBlock(vm.codec, summaryBytes)
	if err != nil {
		return nil, err
	}

	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	return &block.Summary{
		SummaryKey:   commonEng.SummaryKey(summaryBlk.BlockNumber),
		SummaryID:    commonEng.SummaryID(summaryID),
		ContentBytes: summaryBytes,
	}, err
}

// StateSyncGetLastSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by core.CommitInterval.
// Called on nodes that serve fast sync data.
func (vm *stateSyncer) StateSyncGetLastSummary() (commonEng.Summary, error) {
	lastHeight := vm.LastAcceptedBlock().Height()
	lastSyncableBlockNumber := lastHeight - lastHeight%vm.syncableInterval

	syncableBlock, err := vm.stateSummaryAtHeight(lastSyncableBlockNumber)
	if err != nil {
		return nil, err
	}

	return syncableBlockToSummary(vm.codec, syncableBlock)
}

// StateSyncGetSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [key] if the node can serve state sync data for that key.
func (vm *stateSyncer) StateSyncGetSummary(key commonEng.SummaryKey) (commonEng.Summary, error) {
	summaryBlock := vm.chain.GetBlockByNumber(uint64(key))
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > vm.LastAcceptedBlock().Height() ||
		summaryBlock.NumberU64()%vm.syncableInterval != 0 {
		return nil, commonEng.ErrUnknownStateSummary
	}

	syncableBlock, err := vm.stateSummaryAtHeight(summaryBlock.NumberU64())
	if err != nil {
		return nil, err
	}

	return syncableBlockToSummary(vm.codec, syncableBlock)
}

// StateSync performs state sync given a list of summaries
// List of summaries are expected to be in descending order of weight
// Performs state sync on a background go routine to avoid blocking engine.
// Returns before state sync is completed
func (vm *stateSyncer) StateSync(summaries []commonEng.Summary) error {
	log.Info("Starting state sync", "summaries", len(summaries))
	go func() {
		completed, err := vm.stateSync(summaries)
		if err != nil {
			vm.stateSyncError = err
			log.Error("error occurred in stateSync", "err", err)
		}
		if completed {
			// notify engine regardless of whether err == nil,
			// engine will call GetLastSummaryBlockID to retrieve the ID and check the error.
			log.Info("stateSync completed, notifying engine")
			vm.toEngine <- commonEng.StateSyncDone
		} else {
			log.Info("stateSync skipped, notifying engine")
			vm.toEngine <- commonEng.StateSyncSkipped
		}
	}()
	return nil
}

// getSummaryToSync returns summary, block to sync and whether it is a resume from existing summary
// Expects parameter proposedSummaries to be a list of Summary sorted by stake weight (descending order)
// Expects parameter proposedSummaries to contain at least one Summary
// Updates [stateSyncSummaryKey] to the selected summary on disk if needed.
// Returns either an existing Summary from a previous run (if set in DB) if within proposedSummaries
// or first Summary from proposedSummaries (max stake weight).
// Return bool value resume is set to true if returned summary is local and active (contained within proposedSummaries)
// Returns error if:
// - Local or Summary in proposedSummaries could not be marshalled
// - Local or Summary in proposedSummaries could not be verified
// - Other DB related error
func (vm *stateSyncer) getSummaryToSync(proposedSummaries []commonEng.Summary) ([]byte, message.SyncableBlock, bool, error) {
	// Assume summaryToSync is the first of the proposedSummaries (with maximum stake weight)
	summaryToSync := proposedSummaries[0] // by default prefer first summary
	stateSyncBlock, err := summaryToSyncableBlock(vm.codec, summaryToSync.Bytes())
	if err != nil {
		return nil, message.SyncableBlock{}, false, err
	}

	// check if we have saved summary in the DB from previous run
	localSummaryBytes, err := vm.db.Get(stateSyncSummaryKey)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, message.SyncableBlock{}, false, err
	}

	if err == nil {
		// If it exists in the DB, check if it is still supported by peers
		// A summary is supported if it is in the proposedSummaries list
		localSyncableBlock, err := summaryToSyncableBlock(vm.codec, localSummaryBytes)
		if err != nil {
			return nil, message.SyncableBlock{}, false, fmt.Errorf("failed to parse saved state sync summary to SyncableBlock: %w", err)
		}

		// if local summary is in preferred summary, resume state sync using the local summary
		if containsSummary(proposedSummaries, localSummaryBytes) {
			return localSummaryBytes, localSyncableBlock, true, nil
		}
	}
	// marshal the summaryToSync and save it in DB for next time
	if err = vm.db.Put(stateSyncSummaryKey, summaryToSync.Bytes()); err != nil {
		log.Error("error saving state sync summary to disk", "err", err)
		return nil, message.SyncableBlock{}, false, err
	}

	return summaryToSync.Bytes(), stateSyncBlock, false, err
}

// wipeSnapshotIfNeeded clears resume related state if needed
func (vm *stateSyncer) wipeSnapshotIfNeeded(resuming bool) error {
	// in the rare case that we have a previous unclean shutdown
	// initializing the blockchain will try to rebuild the snapshot.
	// we need to stop the async snapshot generation, because it will
	// interfere with the sync.
	// We also clear the marker, since state sync will update the
	// snapshot correctly.
	inProgress, err := snapshot.IsSnapshotGenerationInProgress(vm.chaindb)
	if inProgress || err != nil {
		log.Info("unclean shutdown prior to state sync detected, wiping snapshot", "err", err)
		vm.chain.BlockChain().Snapshots().AbortGeneration()
		snapshot.ResetSnapshotGeneration(vm.chaindb)
		<-snapshot.WipeSnapshot(vm.chaindb, false)
	}

	if !resuming {
		log.Info("not resuming a previous sync, wiping snapshot")
		<-snapshot.WipeSnapshot(vm.chaindb, false)
	}

	return nil
}

// stateSync blockingly performs the state sync for the EVM state and the atomic state
// returns true if state sync was completed/attempted and false if it was skipped, along with
// an error if one occurred.
func (vm *stateSyncer) stateSync(summaries []commonEng.Summary) (bool, error) {
	var (
		resuming bool
		err      error
	)

	if len(summaries) == 0 {
		log.Info("no summaries available, skipping state sync")
		return false, nil
	}

	vm.stateSyncSummary, vm.stateSyncBlock, resuming, err = vm.getSummaryToSync(summaries)
	if err != nil {
		return true, err
	}

	// ensure blockchain is significantly ahead of local last accepted block,
	// to make sure state sync is worth it.
	lastAcceptedHeight := vm.LastAcceptedBlock().Height()
	if lastAcceptedHeight+vm.minBlocks > vm.stateSyncBlock.BlockNumber {
		log.Info(
			"last accepted too close to most recent syncable block, skipping state sync",
			"lastAccepted", lastAcceptedHeight,
			"syncableHeight", vm.stateSyncBlock.BlockNumber,
		)
		return false, nil
	}

	if err = vm.wipeSnapshotIfNeeded(resuming); err != nil {
		return true, err
	}
	if err = vm.db.Commit(); err != nil {
		return true, err
	}

	if err = vm.syncBlocks(vm.stateSyncBlock.BlockHash, vm.stateSyncBlock.BlockNumber, parentsToGet); err != nil {
		return true, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	vm.cancel = cancel
	defer cancel()

	// Here we sync the atomic trie and the EVM state trie. These steps could be done
	// in parallel or in the opposite order. Keeping them serial for simplicity for now.
	if err = vm.syncAtomicTrie(ctx); err != nil {
		return true, err
	}
	return true, vm.syncStateTrie(ctx)
}

func (vm *stateSyncer) syncAtomicTrie(ctx context.Context) error {
	log.Info("atomic tx: sync starting", "root", vm.stateSyncBlock.AtomicRoot)
	vm.syncer = vm.atomicTrie.Syncer(vm.client, vm.stateSyncBlock.AtomicRoot, vm.stateSyncBlock.BlockNumber)
	vm.syncer.Start(ctx)
	err := <-vm.syncer.Done()
	log.Info("atomic tx: sync finished", "root", vm.stateSyncBlock.AtomicRoot, "err", err)
	return err
}

func (vm *stateSyncer) syncStateTrie(ctx context.Context) error {
	log.Info("state sync: sync starting", "root", vm.stateSyncBlock.BlockRoot)
	syncer, err := statesync.NewEVMStateSyncer(&statesync.EVMStateSyncerConfig{
		Client: vm.client,
		Root:   vm.stateSyncBlock.BlockRoot,
		DB:     vm.chaindb,
	})
	if err != nil {
		return err
	}
	vm.syncer = syncer
	vm.syncer.Start(ctx)
	err = <-vm.syncer.Done()
	log.Info("state sync: sync finished", "root", vm.stateSyncBlock.BlockRoot, "err", err)
	return err
}

func containsSummary(summaries []commonEng.Summary, summaryBytes []byte) bool {
	for _, s := range summaries {
		if bytes.Equal(s.Bytes(), summaryBytes) {
			return true
		}
	}

	return false
}

// syncBlocks fetches (up to) [parentsToGet] blocks from peers
// using [client] and writes them to disk.
// the process begins with [fromHash] and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
func (vm *stateSyncer) syncBlocks(fromHash common.Hash, fromHeight uint64, parentsToGet int) error {
	nextHash := fromHash
	nextHeight := fromHeight
	parentsPerRequest := uint16(32)

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for parentsToGet >= 0 {
		blk := vm.chain.GetBlockByHash(nextHash)
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
	batch := vm.chaindb.NewBatch()
	for i := parentsToGet - 1; i >= 0 && (nextHash != common.Hash{}); {
		blocks, err := vm.client.GetBlocks(nextHash, nextHeight, parentsPerRequest)
		if err != nil {
			log.Warn("could not get blocks from peer", "err", err, "nextHash", nextHash, "remaining", i+1)
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

// GetLastSummaryBlockID returns the ID of the block synced to and an
// error if one occurred during syncing.
// Engine calls this function after state sync is done and VM has sent
// the StateSyncDone message to Engine.
func (vm *stateSyncer) GetLastSummaryBlockID() (ids.ID, error) {
	var summary block.CoreSummaryContent
	if _, err := vm.codec.Unmarshal(vm.stateSyncSummary, &summary); err != nil {
		return ids.Empty, err
	}

	return summary.BlkID, vm.stateSyncError
}

// SetLastSummaryBlock sets the given container bytes as the last summary block
// Engine invokes this method after state sync has completed copying information from
// peers. It is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (vm *stateSyncer) SetLastSummaryBlock(blockBytes []byte) error {
	block, err := vm.ParseBlock(blockBytes)
	if err != nil {
		return fmt.Errorf("error parsing block, blockBytes=%s, err=%w", common.Bytes2Hex(blockBytes), err)
	}
	wrapper, ok := block.(*chain.BlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
	}
	evmBlock, ok := wrapper.Block.(*Block)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to evm.Block", block)
	}

	// BloomIndexer needs to know that some parts of the chain are not available
	// and cannot be indexed. This is done by calling [AddCheckpoint] here.
	// Since the indexer uses sections of size [params.BloomBitsBlocks] (= 4096),
	// each block is indexed in section number [blockNumber/params.BloomBitsBlocks].
	// To allow the indexer to start with the block we just synced to,
	// we create a checkpoint for its parent.
	// Note: This requires assuming the synced block height is divisible
	// by [params.BloomBitsBlocks].
	parentHeight := evmBlock.Height() - 1
	parentHash := evmBlock.ethBlock.ParentHash()
	vm.chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err = vm.chain.BlockChain().ResetState(evmBlock.ethBlock); err != nil {
		return fmt.Errorf("error resetting blockchain state, height=%d, hash=%s, err=%w", block.Height(), evmBlock.ethBlock.Hash(), err)
	}
	if err := vm.updateVMMarkers(); err != nil {
		return fmt.Errorf("error updating vm markers, height=%d, hash=%s, err=%w", block.Height(), evmBlock.ethBlock.Hash(), err)
	}
	if err := vm.State.SetLastAcceptedBlock(evmBlock); err != nil {
		return fmt.Errorf("error setting chain state, height=%d, hash=%s, err=%w", block.Height(), evmBlock.ethBlock.Hash(), err)
	}
	log.Info("SetLastSummaryBlock updated VM state", "height", evmBlock.Height(), "hash", evmBlock.ethBlock.Hash())
	// the chain state is already restored, and from this point on
	// the block synced to is the accepted block. the last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	return vm.atomicTrie.ApplyToSharedMemory(evmBlock.ethBlock.NumberU64())
}

// updateVMMarkers updates the following markers in the VM's database
// and commits them atomically:
// - updates atomic trie so it will have necessary metadata for the last committed root
// - updates atomic trie so it will resume applying operations to shared memory on initialize
// - updates lastAcceptedKey
// - removes state sync progress markers
func (vm *stateSyncer) updateVMMarkers() error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block to [vm.stateSyncBlock] when ApplyToSharedMemory
	// is called.
	if err := vm.atomicTrie.MarkApplyToSharedMemoryCursor(vm.LastAcceptedBlock().Height()); err != nil {
		return err
	}
	if err := vm.acceptedBlockDB.Put(lastAcceptedKey, vm.stateSyncBlock.BlockHash[:]); err != nil {
		return err
	}
	if err := vm.db.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *stateSyncer) Shutdown() error {
	// [vm.cancel] is nil if we get here before starting the atomic trie sync
	if vm.cancel != nil {
		vm.cancel()
	}
	return nil
}
