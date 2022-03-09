// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync"
	"github.com/ava-labs/coreth/statesync/stats"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// state sync commit cap
	commitCap = 1 * units.MiB

	// maximum number of blocks to fetch after the fast sync block
	// Note: The last 256 block hashes are necessary to support
	// the BLOCKHASH opcode.
	parentsToGet = 256

	// maximum number of retry attempts for a single state sync request
	maxRetryAttempts = uint8(32)
)

var (
	stateSyncSummaryKey = []byte("stateSyncSummary")
	atomicSyncDoneKey   = []byte("atomicSyncDone")
	stateSyncDoneKey    = []byte("stateSyncDone")
)

type stateSyncConfig struct {
	enabled      bool
	codec        codec.Manager
	netCodec     codec.Manager
	network      peer.Network
	client       peer.Client
	toEngine     chan<- commonEng.Message
	syncStats    stats.Stats
	stateSyncIDs []ids.ShortID
}

type stateSyncer struct {
	syncer           statesync.Syncer
	stateSyncError   error
	stateSyncSummary commonEng.Summary
	stateSyncBlock   message.SyncableBlock

	client statesync.Client
	*stateSyncConfig
	*vmState

	// used to stop syncer
	cancel func()
}

func NewStateSyncer(vmState *vmState, config *stateSyncConfig) *stateSyncer {
	return &stateSyncer{
		vmState:         vmState,
		stateSyncConfig: config,
		client:          statesync.NewClient(config.client, maxRetryAttempts, statesync.DefaultMaxRetryDelay, config.netCodec, config.stateSyncIDs),
	}
}

func (vm *stateSyncer) stateSummaryAtHeight(height uint64) (message.SyncableBlock, error) {
	atomicHash, err := vm.atomicTrie.Root(height)
	if err != nil {
		return message.SyncableBlock{}, fmt.Errorf("error getting atomic trie root for height (%d): %w", height, err)
	}

	if (atomicHash == common.Hash{}) {
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
		AtomicRoot:  atomicHash,
		BlockHash:   blk.Hash(),
		BlockRoot:   blk.Root(),
		BlockNumber: height,
	}
	log.Info("sync roots returned", "roots", response, "height", height)
	return response, nil
}

// StateSyncEnabled returns whether the state sync is enabled on this VM
func (vm *stateSyncer) StateSyncEnabled() (bool, error) {
	return vm.enabled, nil
}

// StateSyncGetKeyHash implements StateSyncableVM and extracts the key and computes a hash for
// verification from [summary].
func (vm *VM) StateSyncGetKeyHash(summary commonEng.Summary) (commonEng.SummaryKey, commonEng.SummaryHash, error) {
	summaryBlk, err := summaryToSyncableBlock(vm.codec, summary)
	if err != nil {
		return commonEng.SummaryKey{}, commonEng.SummaryHash{}, err
	}
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, summaryBlk.BlockNumber)

	return heightBytes, crypto.Keccak256(summary), nil
}

// StateSyncGetLastSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by core.CommitInterval.
// Called on nodes that serve fast sync data.
func (vm *stateSyncer) StateSyncGetLastSummary() (commonEng.Summary, error) {
	lastHeight := vm.LastAcceptedBlock().Height()
	lastSyncableBlockNumber := lastHeight - lastHeight%core.CommitInterval

	syncableBlock, err := vm.stateSummaryAtHeight(lastSyncableBlockNumber)
	if err != nil {
		return commonEng.Summary{}, err
	}

	return syncableBlockToSummary(vm.codec, syncableBlock)
}

// StateSyncGetSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [key] if the node can serve state sync data for that key.
func (vm *stateSyncer) StateSyncGetSummary(key commonEng.SummaryKey) (commonEng.Summary, error) {
	if len(key) != wrappers.LongLen {
		return commonEng.Summary{}, fmt.Errorf("unexpected summary key len %d", len(key))
	}
	height := binary.BigEndian.Uint64(key)
	summaryBlock := vm.chain.GetBlockByNumber(height)
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > vm.LastAcceptedBlock().Height() ||
		summaryBlock.NumberU64()%core.CommitInterval != 0 {
		return commonEng.Summary{}, commonEng.ErrUnknownStateSummary
	}

	syncableBlock, err := vm.stateSummaryAtHeight(summaryBlock.NumberU64())
	if err != nil {
		return commonEng.Summary{}, err
	}

	return syncableBlockToSummary(vm.codec, syncableBlock)
}

// StateSync performs state sync given a list of summaries
// List of summaries are expected to be in descending order of weight
// Performs state sync on a background go routine to avoid blocking engine.
// Returns before state sync is completed
func (vm *stateSyncer) StateSync(summaries []commonEng.Summary) error {
	// TODO: check edge case where node is restarted after fully completed state sync
	// does it try to sync to state root older than last accepted? (if chain hasn't moved yet)
	if len(summaries) == 0 {
		log.Info("no summaries in state sync, skipping")
		return nil
	}

	log.Info("Starting state sync", "summaries", len(summaries))
	go vm.stateSync(summaries)
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
func (vm *stateSyncer) getSummaryToSync(proposedSummaries []commonEng.Summary) (commonEng.Summary, message.SyncableBlock, bool, error) {
	// Assume summaryToSync is the first of the proposedSummaries (with maximum stake weight)
	summaryToSync := proposedSummaries[0] // by default prefer first summary
	stateSyncBlock, err := summaryToSyncableBlock(vm.codec, summaryToSync)
	if err != nil {
		return commonEng.Summary{}, message.SyncableBlock{}, false, err
	}

	// check if we have saved summary in the DB from previous run
	localSummaryBytes, err := vm.db.Get(stateSyncSummaryKey)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return commonEng.Summary{}, message.SyncableBlock{}, false, err
	}

	if err == nil && len(localSummaryBytes) > 0 {
		// If it exists in the DB, check if it is still supported by peers
		// A summary is supported if it is in the proposedSummaries list
		var localSummary commonEng.Summary
		if _, err = vm.codec.Unmarshal(localSummaryBytes, &localSummary); err != nil {
			return commonEng.Summary{}, message.SyncableBlock{}, false, fmt.Errorf("failed to unmarshal saved state sync summary: %w", err)
		}

		localSyncableBlock, err := summaryToSyncableBlock(vm.codec, localSummary)
		if err != nil {
			return commonEng.Summary{}, message.SyncableBlock{}, false, fmt.Errorf("failed to parse saved state sync summary to SyncableBlock: %w", err)
		}

		// if local summary is in preferred summary, resume state sync using the local summary
		if containsSummary(proposedSummaries, localSummary) {
			return localSummary, localSyncableBlock, true, nil
		}
	}
	// marshal the summaryToSync and save it in DB for next time
	stateSyncSummaryBytes, err := vm.codec.Marshal(codecVersion, summaryToSync)
	if err != nil {
		log.Error("failed to marshal state summary bytes", "err", err)
		return commonEng.Summary{}, message.SyncableBlock{}, false, err
	}

	if err = vm.db.Put(stateSyncSummaryKey, stateSyncSummaryBytes); err != nil {
		log.Error("error saving state sync summary to disk", "err", err)
		return commonEng.Summary{}, message.SyncableBlock{}, false, err
	}

	return summaryToSync, stateSyncBlock, false, err
}

// stateSync blockingly performs the state sync for both the eth state and the atomic state
// Notifies engine commonEng.StateSyncDone regardless of the state sync result
func (vm *stateSyncer) stateSync(summaries []commonEng.Summary) {
	// notify engine at the end of this function regardless of success
	// engine will call GetLastSummaryBlockID to retrieve the ID
	// of the successfully synced block and the error result if one occurred.
	defer func() {
		if vm.stateSyncError != nil {
			log.Error("error occurred in stateSync, notifying engine", "err", vm.stateSyncError)
		} else {
			log.Info("stateSync completed successfully, notifying engine")
		}
		vm.toEngine <- commonEng.StateSyncDone
	}()

	var (
		resuming bool
		err      error
	)

	vm.stateSyncSummary, vm.stateSyncBlock, resuming, err = vm.getSummaryToSync(summaries)
	if err != nil {
		vm.stateSyncError = err
		return
	}

	if !resuming {
		log.Info("not resuming a previous sync, wipe snapshot & resume markers")
		<-snapshot.WipeSnapshot(vm.chaindb, false)
		if err := vm.db.Delete(atomicSyncDoneKey); err != nil {
			vm.stateSyncError = err
			return
		}
		if err := vm.db.Delete(stateSyncDoneKey); err != nil {
			vm.stateSyncError = err
			return
		}
	}

	if err = vm.db.Commit(); err != nil {
		vm.stateSyncError = err
		return
	}

	if err := vm.syncBlocks(vm.stateSyncBlock.BlockHash, vm.stateSyncBlock.BlockNumber, parentsToGet); err != nil {
		vm.stateSyncError = err
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	vm.cancel = cancel
	defer cancel()

	atomicSync := func() error { return vm.syncAtomicTrie(ctx, commitCap) }
	if err := vm.doWithMarker(atomicSyncDoneKey, "atomic sync", atomicSync); err != nil {
		vm.stateSyncError = err
		return
	}

	stateSync := func() error { return vm.syncStateTrie(ctx, commitCap) }
	if err := vm.doWithMarker(stateSyncDoneKey, "state sync", stateSync); err != nil {
		vm.stateSyncError = err
		return
	}
}

// doWithMarker executes work function if marker specified in doneKey is not set
// Sets marker in doneKey if work function returns no errors
// Does nothing if marker in doneKey is already set
// name is for logging purposes only
func (vm *stateSyncer) doWithMarker(doneKey []byte, name string, work func() error) error {
	if done, err := vm.db.Has(doneKey); err != nil {
		return err
	} else if done {
		log.Info("skipping already completed sync step", "step", name)
		return nil
	}
	if err := work(); err != nil {
		log.Error("could not complete sync step", "step", name)
		return err
	}
	if err := vm.db.Put(doneKey, nil); err != nil {
		return err
	}
	log.Info("sync step completed", "step", name)
	return vm.db.Commit()
}

func (vm *stateSyncer) syncAtomicTrie(ctx context.Context, commitCap int) error {
	log.Info("atomic tx: sync starting", "root", vm.stateSyncBlock.AtomicRoot)
	vm.syncer = statesync.NewLeafSyncer(
		message.AtomicTrieNode, 40, vm.atomicTrie.TrieDB().DiskDB(), vm.db.Commit, commitCap, nil, vm.client, 1, stats.NewStats())
	vm.syncer.Start(ctx, vm.stateSyncBlock.AtomicRoot)
	<-vm.syncer.Done()
	log.Info("atomic tx: sync finished", "root", vm.stateSyncBlock.AtomicRoot, "err", vm.syncer.Error())
	return vm.syncer.Error()
}

func (vm *stateSyncer) syncStateTrie(ctx context.Context, commitCap int) error {
	log.Info("state sync: sync starting", "root", vm.stateSyncBlock.BlockRoot)
	progressMarker, err := statesync.NewProgressMarker(vm.chaindb, vm.netCodec, 4)
	if err != nil {
		return err
	}

	vm.syncer = statesync.NewLeafSyncer(
		message.StateTrieNode, 32, vm.chaindb, nil, commitCap, progressMarker, vm.client, 4, stats.NewStats())
	vm.syncer.Start(ctx, vm.stateSyncBlock.BlockRoot)
	<-vm.syncer.Done()

	log.Info("state sync: sync finished", "root", vm.stateSyncBlock.BlockRoot, "err", vm.syncer.Error())
	return vm.syncer.Error()
}

func containsSummary(summaries []commonEng.Summary, summary commonEng.Summary) bool {
	for _, s := range summaries {
		if bytes.Equal(s, summary) {
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
	lastUpdate := time.Now()
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
		if time.Since(lastUpdate) > 8*time.Second {
			log.Info("fetching blocks from peer", "remaining", i+1, "total", parentsToGet)
			lastUpdate = time.Now()
		}
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
	if err := vm.atomicTrie.UpdateLastCommitted(vm.stateSyncBlock.AtomicRoot, vm.stateSyncBlock.BlockNumber); err != nil {
		return err
	}
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block to [vm.stateSyncBlock] when ApplyToSharedMemory
	// is called.
	if err := vm.atomicTrie.MarkApplyToSharedMemoryCursor(vm.LastAcceptedBlock().Height()); err != nil {
		return err
	}
	if err := vm.acceptedBlockDB.Put(lastAcceptedKey, vm.stateSyncBlock.BlockHash[:]); err != nil {
		return err
	}
	if err := vm.db.Delete(atomicSyncDoneKey); err != nil {
		return err
	}
	if err := vm.db.Delete(stateSyncDoneKey); err != nil {
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
