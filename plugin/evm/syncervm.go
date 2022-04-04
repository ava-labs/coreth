// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/ethdb"
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
	// State sync fetches [parentsToGet] parents of the block it syncs to.
	// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
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
	state *vmState

	// Enable fast sync, falls back to original bootstrapping algorithm if false.
	enabled bool

	// statsEnabled controls whether stats are emitted during state sync
	statsEnabled bool

	// Force state syncer to select the highest available summary block regardless of
	// whether the local summary block is available.
	forceSyncHighestSummary bool

	// netCodec is used to encode and decode network messages
	netCodec codec.Manager

	// client is used to contact peers during state sync
	client peer.NetworkClient

	// toEngine is the channel that syncerVM uses to notify engine when the sync
	// operation was performed (StateSyncDone message) or skipped (StateSyncSkipped message)
	toEngine chan<- commonEng.Message

	// stateSyncIDs is the list of IDs to perform state sync from
	// If empty, state sync is performed across the entire network.
	stateSyncIDs []ids.ShortID

	// minBlocks is the minimum number of blocks our last accepted block should be behind the network
	// for us to perform state sync instead of falling back to the normal bootstrapping algorithm.
	minBlocks uint64

	// syncableInterval is the interval over block heights for which we keep a state summary.
	// Ie if the block height is divisible by [syncableInterval] it is eligible for supporting a state
	// summary at that block.
	syncableInterval uint64

	// localSyncableBlock is set to the SyncableBlock corresponding to a state summary
	// that was previously started and we can possibly resume to, if still supported by peers.
	localSyncableBlock message.SyncableBlock
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

	// Client to request data from the network
	client syncclient.Client

	// These fields are used to return the results of state sync
	stateSyncBlock message.SyncableBlock
	stateSyncError error

	// used to stop syncer
	cancel func()
}

func NewStateSyncer(config *stateSyncConfig) *stateSyncer {
	return &stateSyncer{
		stateSyncConfig: config,
		client: syncclient.NewClient(&syncclient.ClientConfig{
			NetworkClient:    config.client,
			Codec:            config.netCodec,
			Stats:            stats.NewStats(config.statsEnabled),
			MaxAttempts:      maxRetryAttempts,
			MaxRetryDelay:    defaultMaxRetryDelay,
			StateSyncNodeIDs: config.stateSyncIDs,
		}),
	}
}

func (vm *stateSyncer) stateSummaryAtHeight(height uint64) (message.SyncableBlock, error) {
	atomicRoot, err := vm.state.atomicTrie.Root(height)
	if err != nil {
		return message.SyncableBlock{}, fmt.Errorf("error getting atomic trie root for height (%d): %w", height, err)
	}

	if (atomicRoot == common.Hash{}) {
		return message.SyncableBlock{}, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	blk := vm.state.chain.GetBlockByNumber(height)
	if blk == nil {
		return message.SyncableBlock{}, fmt.Errorf("block not found for height (%d)", height)
	}

	if !vm.state.chain.BlockChain().HasState(blk.Root()) {
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
func (vm *stateSyncer) ParseSummary(summaryBytes []byte) (commonEng.Summary, error) {
	summaryBlk, err := parseSummary(vm.netCodec, summaryBytes)
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

// GetOngoingStateSyncSummary is called by engine so the state sync summary
// we could resume gets included in summaries passed to [StateSync], if this
// summary is still supported by enough peers.
// returns ErrNoStateSyncOngoing if no in progress summary is available.
func (vm *stateSyncer) GetOngoingStateSyncSummary() (commonEng.Summary, error) {
	localSummaryBytes, err := vm.state.metadataDB.Get(stateSyncSummaryKey)
	if errors.Is(err, database.ErrNotFound) {
		return nil, commonEng.ErrNoStateSyncOngoing
	} else if err != nil {
		return nil, err
	}

	vm.localSyncableBlock, err = parseSummary(vm.netCodec, localSummaryBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncableBlock: %w", err)
	}

	summaryID, err := ids.ToID(crypto.Keccak256(localSummaryBytes))
	return &block.Summary{
		SummaryKey:   commonEng.SummaryKey(vm.localSyncableBlock.BlockNumber),
		SummaryID:    commonEng.SummaryID(summaryID),
		ContentBytes: localSummaryBytes,
	}, err
}

// StateSyncGetLastSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by core.CommitInterval.
// Called on nodes that serve state sync data.
func (vm *stateSyncer) StateSyncGetLastSummary() (commonEng.Summary, error) {
	lastHeight := vm.state.LastAcceptedBlock().Height()
	lastSyncableBlockNumber := lastHeight - lastHeight%vm.syncableInterval

	syncableBlock, err := vm.stateSummaryAtHeight(lastSyncableBlockNumber)
	if err != nil {
		return nil, err
	}

	return syncableBlockToSummary(vm.netCodec, syncableBlock)
}

// StateSyncGetSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [key] if the node can serve state sync data for that key.
func (vm *stateSyncer) StateSyncGetSummary(key commonEng.SummaryKey) (commonEng.Summary, error) {
	summaryBlock := vm.state.chain.GetBlockByNumber(uint64(key))
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > vm.state.LastAcceptedBlock().Height() ||
		summaryBlock.NumberU64()%vm.syncableInterval != 0 {
		return nil, commonEng.ErrUnknownStateSummary
	}

	syncableBlock, err := vm.stateSummaryAtHeight(summaryBlock.NumberU64())
	if err != nil {
		return nil, err
	}

	return syncableBlockToSummary(vm.netCodec, syncableBlock)
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

// selectSyncSummary returns the summary block to sync, whether it is resuming an ongoing operation, and an error
// if a summary block could not be determined.
// Assumes that [proposedSummaries] to be a non-empty list of the summaries that received an alpha threshold of votes
// from the network.
// Updates [stateSummaryKey] to the selected summary on disk if changed from its previous value.
// Note: selectSyncSummary will return the proposed summary with the highest block number unless there is an ongoing sync
// for a syncable block included in [proposedSummaries]
func (vm *stateSyncer) selectSyncSummary(proposedSummaries []commonEng.Summary) (message.SyncableBlock, bool, error) {
	var (
		highestSummaryBlock      message.SyncableBlock
		highestSummaryBlockBytes []byte
	)

	for _, proposedSummary := range proposedSummaries {
		syncableBlock, err := parseSummary(vm.netCodec, proposedSummary.Bytes())
		if err != nil {
			return message.SyncableBlock{}, false, err
		}

		// If the actively syncing block is included in [proposedSummaries] resume syncing it
		// unless [forceSyncHighestSummary] is enabled.
		if vm.localSyncableBlock.BlockHash == syncableBlock.BlockHash && !vm.forceSyncHighestSummary {
			return vm.localSyncableBlock, true, nil
		}

		if syncableBlock.BlockNumber > highestSummaryBlock.BlockNumber {
			highestSummaryBlock = syncableBlock
			highestSummaryBlockBytes = proposedSummary.Bytes()
		}
	}

	// Otherwise, update the current state sync summary key in the database
	if err := vm.state.metadataDB.Put(stateSyncSummaryKey, highestSummaryBlockBytes); err != nil {
		return message.SyncableBlock{}, false, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}

	return highestSummaryBlock, false, nil
}

// wipeSnapshotIfNeeded wipes the snapshot and resets the snapshot generation state if needed.
func (vm *stateSyncer) wipeSnapshotIfNeeded(resuming bool) error {
	// In the rare case of a previous unclean shutdown, initializing
	// the chain will try to rebuild the snapshot. We need to stop the
	// async snapshot generation, because it interferes with the sync.
	// We also clear the marker, since state sync will update the
	// snapshot correctly.
	inProgress, err := snapshot.IsSnapshotGenerationInProgress(vm.state.chaindb)
	if inProgress || err != nil {
		log.Info("unclean shutdown prior to state sync detected, wiping snapshot", "err", err)
		vm.state.chain.BlockChain().Snapshots().AbortGeneration()
		snapshot.ResetSnapshotGeneration(vm.state.chaindb)
		<-snapshot.WipeSnapshot(vm.state.chaindb, false)
	}

	if !resuming {
		log.Info("not resuming a previous sync, wiping snapshot")
		<-snapshot.WipeSnapshot(vm.state.chaindb, false)
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

	vm.stateSyncBlock, resuming, err = vm.selectSyncSummary(summaries)
	if err != nil {
		return true, err
	}

	// ensure blockchain is significantly ahead of local last accepted block,
	// to make sure state sync is worth it.
	// Note: this additionally ensures we do not mistakenly attempt to fast sync to a block
	// behind our own last accepted block.
	lastAcceptedHeight := vm.state.LastAcceptedBlock().Height()
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
	if err = vm.state.db.Commit(); err != nil {
		return true, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	vm.cancel = cancel
	defer cancel()

	if err = vm.syncBlocks(ctx, vm.stateSyncBlock.BlockHash, vm.stateSyncBlock.BlockNumber, parentsToGet); err != nil {
		return true, err
	}

	// Here we sync the atomic trie and the EVM state trie. These steps could be done
	// in parallel or in the opposite order. Keeping them serial for simplicity for now.
	if err = vm.syncAtomicTrie(ctx); err != nil {
		return true, err
	}
	return true, vm.syncStateTrie(ctx)
}

func (vm *stateSyncer) syncAtomicTrie(ctx context.Context) error {
	log.Info("atomic tx: sync starting", "root", vm.stateSyncBlock.AtomicRoot)
	atomicSyncer := vm.state.atomicTrie.Syncer(vm.client, vm.stateSyncBlock.AtomicRoot, vm.stateSyncBlock.BlockNumber)
	atomicSyncer.Start(ctx)
	err := <-atomicSyncer.Done()
	log.Info("atomic tx: sync finished", "root", vm.stateSyncBlock.AtomicRoot, "err", err)
	return err
}

func (vm *stateSyncer) syncStateTrie(ctx context.Context) error {
	log.Info("state sync: sync starting", "root", vm.stateSyncBlock.BlockRoot)
	evmSyncer, err := statesync.NewEVMStateSyncer(&statesync.EVMStateSyncerConfig{
		Client:    vm.client,
		Root:      vm.stateSyncBlock.BlockRoot,
		DB:        vm.state.chaindb,
		BatchSize: ethdb.IdealBatchSize,
	})
	if err != nil {
		return err
	}
	evmSyncer.Start(ctx)
	err = <-evmSyncer.Done()
	log.Info("state sync: sync finished", "root", vm.stateSyncBlock.BlockRoot, "err", err)
	return err
}

// syncBlocks fetches (up to) [parentsToGet] blocks from peers
// using [client] and writes them to disk.
// the process begins with [fromHash] and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
func (vm *stateSyncer) syncBlocks(ctx context.Context, fromHash common.Hash, fromHeight uint64, parentsToGet int) error {
	nextHash := fromHash
	nextHeight := fromHeight
	parentsPerRequest := uint16(32)

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for parentsToGet >= 0 {
		blk := vm.state.chain.GetBlockByHash(nextHash)
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
	batch := vm.state.chaindb.NewBatch()
	for i := parentsToGet - 1; i >= 0 && (nextHash != common.Hash{}); {
		if err := ctx.Err(); err != nil {
			return err
		}
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

// GetStateSyncResult returns the ID of the block synced to, its height, and an
// error if one occurred during syncing.
// Engine calls this method after VM has StateSyncDone to Engine.
func (vm *stateSyncer) GetStateSyncResult() (ids.ID, uint64, error) {
	return ids.ID(vm.stateSyncBlock.BlockHash), vm.stateSyncBlock.BlockNumber, vm.stateSyncError
}

// SetLastSummaryBlock sets the given container bytes as the last summary block
// Engine invokes this method after state sync has completed copying information from
// peers. It is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (vm *stateSyncer) SetLastSummaryBlock(blockBytes []byte) error {
	block, err := vm.state.ParseBlock(blockBytes)
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
	evmBlock.SetStatus(choices.Accepted)

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
	vm.state.chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err = vm.state.chain.BlockChain().ResetState(evmBlock.ethBlock); err != nil {
		return fmt.Errorf("error resetting blockchain state, height=%d, hash=%s, err=%w", block.Height(), evmBlock.ethBlock.Hash(), err)
	}
	if err := vm.updateVMMarkers(); err != nil {
		return fmt.Errorf("error updating vm markers, height=%d, hash=%s, err=%w", block.Height(), evmBlock.ethBlock.Hash(), err)
	}
	if err := vm.state.SetLastAcceptedBlock(evmBlock); err != nil {
		return fmt.Errorf("error setting chain state, height=%d, hash=%s, err=%w", block.Height(), evmBlock.ethBlock.Hash(), err)
	}
	log.Info("SetLastSummaryBlock updated VM state", "height", evmBlock.Height(), "hash", evmBlock.ethBlock.Hash())
	// the chain state is already restored, and from this point on
	// the block synced to is the accepted block. the last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	return vm.state.atomicTrie.ApplyToSharedMemory(evmBlock.ethBlock.NumberU64())
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
	if err := vm.state.atomicTrie.MarkApplyToSharedMemoryCursor(vm.state.LastAcceptedBlock().Height()); err != nil {
		return err
	}
	if err := vm.state.acceptedBlockDB.Put(lastAcceptedKey, vm.stateSyncBlock.BlockHash[:]); err != nil {
		return err
	}
	if err := vm.state.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return vm.state.db.Commit()
}

func (vm *stateSyncer) Shutdown() error {
	// [vm.cancel] is nil if we get here before starting the atomic trie sync
	if vm.cancel != nil {
		vm.cancel()
	}
	return nil
}
