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
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// State sync fetches [parentsToGet] parents of the block it syncs to.
	// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
	parentsToGet = 256

	// maximum number of retry attempts for a single state sync request
	maxRetryAttempts = uint8(32)

	// defaultStateSyncMinBlocks is the minimum number of blocks the blockchain
	// should be ahead of local last accepted to perform state sync.
	// This constant is chosen so if normal bootstrapping would be faster than
	// state sync, normal bootstrapping is preferred.
	// time assumptions:
	// - normal bootstrap processing time: ~14 blocks / second
	// - state sync time: ~6 hrs.
	defaultStateSyncMinBlocks = 300_000
)

var (
	// maximum delay in between successive requests
	defaultMaxRetryDelay = 10 * time.Second

	stateSyncSummaryKey = []byte("stateSyncSummary")
)

// stateSyncClientConfig defines the options and dependencies needed to construct a StateSyncerClient
type stateSyncClientConfig struct {
	enabled                 bool
	forceSyncHighestSummary bool

	lastAcceptedHeight uint64
	// Specifies the number of blocks behind the latest state summary that the chain must be
	// in order to prefer performing state sync over falling back to the normal bootstrapping
	// algorithm.
	minBlocksBehindStateSync uint64

	chain           *coreth.ETHChain
	state           *chain.State
	chaindb         ethdb.Database
	metadataDB      database.Database
	acceptedBlockDB database.Database
	db              *versiondb.Database
	atomicTrie      AtomicTrie

	client   syncclient.Client
	netCodec codec.Manager

	toEngine chan<- commonEng.Message
}

type stateSyncerClient struct {
	*stateSyncClientConfig

	resumableSummary message.SyncSummary

	cancel context.CancelFunc

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
	StateSyncEnabled() (bool, error)
	StateSyncGetOngoingSummary() (commonEng.Summary, error)
	StateSyncClearOngoingSummary() error
	StateSyncParseSummary(summaryBytes []byte) (commonEng.Summary, error)
	StateSync(summaries []commonEng.Summary) error
	StateSyncGetResult() (ids.ID, uint64, error)
	StateSyncSetLastSummaryBlock(blockBytes []byte) error
	Shutdown() error
}

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	Start(ctx context.Context)
	Done() <-chan error
}

// StateSyncEnabled always returns true as the client
func (client *stateSyncerClient) StateSyncEnabled() (bool, error) { return client.enabled, nil }

// StateSyncGetOngoingSummary returns a state summary that was previously started
// and not finished, and sets [localSyncableBlock] if one was found
func (client *stateSyncerClient) StateSyncGetOngoingSummary() (commonEng.Summary, error) {
	summaryBytes, err := client.metadataDB.Get(stateSyncSummaryKey)
	if errors.Is(err, database.ErrNotFound) {
		return nil, commonEng.ErrNoStateSyncOngoing
	} else if err != nil {
		return nil, err
	}

	summary, err := message.NewSyncSummaryFromBytes(client.netCodec, summaryBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	client.resumableSummary = summary
	return summary, nil
}

// StateSyncClearOngoingSummary clears any marker of an ongoing state sync summary
func (client *stateSyncerClient) StateSyncClearOngoingSummary() error {
	if err := client.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return fmt.Errorf("failed to clear ongoing summary: %w", err)
	}
	if err := client.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	return nil
}

// StateSyncParseSummary parses [summaryBytes] to [commonEng.Summary]
func (client *stateSyncerClient) StateSyncParseSummary(summaryBytes []byte) (commonEng.Summary, error) {
	return message.NewSyncSummaryFromBytes(client.netCodec, summaryBytes)
}

// StateSync is called from the engine with a slice of [summaries] the
// network will support syncing to.
// The client selects one summary and performs state sync without blocking,
// returning before state sync is completed.
// The client sends a message on [toEngine] after syncing was completed or skipped.
func (client *stateSyncerClient) StateSync(summaries []commonEng.Summary) error {
	log.Info("Starting state sync", "summaries", len(summaries))
	go func() {
		completed, err := client.stateSync(summaries)
		if err != nil {
			client.stateSyncErr = err
			log.Error("error occurred in stateSync", "err", err)
		}
		if completed {
			// notify engine regardless of whether err == nil,
			// engine will call StateSyncGetResult to retrieve the ID and check the error.
			log.Info("stateSync completed, notifying engine")
			client.toEngine <- commonEng.StateSyncDone
		} else {
			log.Info("stateSync skipped, notifying engine")
			// Initialize snapshots if we're skipping state sync, since it will not have been initialized on
			// startup.
			if err := client.StateSyncClearOngoingSummary(); err != nil {
				log.Error("failed to clear ongoing summary after skipping state sync", "err", err)
			}
			client.chain.BlockChain().InitializeSnapshots()
			client.toEngine <- commonEng.StateSyncSkipped
		}
	}()
	return nil
}

// stateSync blockingly performs the state sync for the EVM state and the atomic state
// returns true if state sync was completed/attempted and false if it was skipped, along with
// an error if one occurred.
func (client *stateSyncerClient) stateSync(summaries []commonEng.Summary) (bool, error) {
	if len(summaries) == 0 {
		log.Info("no summaries available, skipping state sync")
		return false, nil
	}

	performSync, err := client.selectSyncSummary(summaries)
	if err != nil {
		return true, err
	}

	if !performSync {
		return false, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	client.cancel = cancel
	defer cancel()

	if err := client.syncBlocks(ctx, client.syncSummary.BlockHash, client.syncSummary.BlockNumber, parentsToGet); err != nil {
		return true, err
	}

	// Sync the EVM trie and then the atomic trie. These steps could be done
	// in parallel or in the opposite order. Keeping them serial for simplicity for now.
	if err := client.syncStateTrie(ctx); err != nil {
		return true, err
	}

	return true, client.syncAtomicTrie(ctx)
}

// selectSyncSummary returns the summary block to sync, whether it is resuming an ongoing operation, and an error
// if a summary block could not be determined.
// Assumes that [proposedSummaries] to be a non-empty list of the summaries that received an alpha threshold of votes
// from the network.
// Updates [stateSummaryKey] to the selected summary on disk if changed from its previous value.
// Note: selectSyncSummary will return the proposed summary with the highest block number unless there is an ongoing sync
// for a syncable block included in [proposedSummaries]
// TODO update comment
func (client *stateSyncerClient) selectSyncSummary(proposedSummaries []commonEng.Summary) (bool, error) {
	var (
		highestSummaryBlock      message.SyncSummary
		highestSummaryBlockBytes []byte
	)

	for _, proposedSummary := range proposedSummaries {
		// Note: we could typecast the summary to syncable block here, but this would complicate serving state sync over gRPC.
		syncSummary, err := message.NewSyncSummaryFromBytes(client.netCodec, proposedSummary.Bytes())
		if err != nil {
			return false, fmt.Errorf("failed to parse syncable block from proposed summary: %w", err)
		}

		// If the actively syncing block is included in [proposedSummaries] resume syncing it
		// unless [forceSyncHighestSummary] is enabled.
		// TODO add a comment explaining why we do not need to wipe the snapshot in this case and we don't need to check
		// the min blocks behind condition
		// Note: we do not need to wipe the snapshot when resuming from the local summary, since it was already wiped
		// when
		if client.resumableSummary.BlockHash == syncSummary.BlockHash && !client.forceSyncHighestSummary {
			client.syncSummary = client.resumableSummary
			return true, nil
		}

		if syncSummary.BlockNumber > highestSummaryBlock.BlockNumber {
			highestSummaryBlock = syncSummary
			highestSummaryBlockBytes = proposedSummary.Bytes()
		}
	}

	// Set the state sync block based on the result.
	client.syncSummary = highestSummaryBlock

	// ensure blockchain is significantly ahead of local last accepted block,
	// to make sure state sync is worth it.
	// Note: this additionally ensures we do not mistakenly attempt to sync to a height
	// less than the client's last accepted block.
	if client.lastAcceptedHeight+client.minBlocksBehindStateSync > client.syncSummary.BlockNumber {
		log.Info(
			"last accepted too close to most recent syncable block, skipping state sync",
			"lastAccepted", client.lastAcceptedHeight,
			"syncableHeight", client.syncSummary.BlockNumber,
		)
		return false, nil
	}

	// Wipe the snapshot completely if we are not resuming from an existing sync, so that we do not
	// use a corrupted snapshot.
	// Note: this assumes that when the node is started with state sync disabled, the in-progress state
	// sync marker will be wiped, so we do not accidentally resume progress from an incorrect version
	// of the snapshot. (if switching between versions that come before this change and back this could
	// lead to the snapshot not being cleaned up correctly)
	if client.syncSummary.BlockHash != client.resumableSummary.BlockHash {
		<-snapshot.WipeSnapshot(client.chaindb, true)
		// Reset the snapshot generator here so that when state sync completes, snapshots will not attempt to read an
		// invalid generator.
		// Note: this must be called after WipeSnapshot is called so that we do not invalidate a partially generated snapshot.
		snapshot.ResetSnapshotGeneration(client.chaindb)
	}

	// Otherwise, update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := client.metadataDB.Put(stateSyncSummaryKey, highestSummaryBlockBytes); err != nil {
		return false, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := client.db.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit db: %w", err)
	}

	return true, nil
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
		blocks, err := client.client.GetBlocks(nextHash, nextHeight, parentsPerRequest)
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

func (client *stateSyncerClient) syncAtomicTrie(ctx context.Context) error {
	log.Info("atomic tx: sync starting", "root", client.syncSummary.AtomicRoot)
	atomicSyncer := client.atomicTrie.Syncer(client.client, client.syncSummary.AtomicRoot, client.syncSummary.BlockNumber)
	atomicSyncer.Start(ctx)
	err := <-atomicSyncer.Done()
	log.Info("atomic tx: sync finished", "root", client.syncSummary.AtomicRoot, "err", err)
	return err
}

func (client *stateSyncerClient) syncStateTrie(ctx context.Context) error {
	log.Info("state sync: sync starting", "root", client.syncSummary.BlockRoot)
	evmSyncer, err := statesync.NewEVMStateSyncer(&statesync.EVMStateSyncerConfig{
		Client:    client.client,
		Root:      client.syncSummary.BlockRoot,
		BatchSize: ethdb.IdealBatchSize,
		DB:        client.chaindb,
	})
	if err != nil {
		return err
	}
	evmSyncer.Start(ctx)
	err = <-evmSyncer.Done()
	log.Info("state sync: sync finished", "root", client.syncSummary.BlockRoot, "err", err)
	return err
}

// At the end of StateSync process, VM will have rebuilt the state of its blockchain
// up to a given height. However the block associated with that height may be not known
// to the VM yet. StateSyncGetResult allows retrival of this block from network
func (client *stateSyncerClient) StateSyncGetResult() (ids.ID, uint64, error) {
	return ids.ID(client.syncSummary.BlockHash), client.syncSummary.BlockNumber, client.stateSyncErr
}

func (client *stateSyncerClient) Shutdown() error {
	if client.cancel != nil {
		client.cancel()
	}
	return nil
}

// StateSyncSetLastSummaryBlock sets the given container bytes as the last summary block
// Engine invokes this method after state sync has completed copying information from
// peers. It is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (client *stateSyncerClient) StateSyncSetLastSummaryBlock(blockBytes []byte) error {
	stateBlock, err := client.state.ParseBlock(blockBytes)
	if err != nil {
		return fmt.Errorf("error parsing block, blockBytes=%s, err=%w", common.Bytes2Hex(blockBytes), err)
	}
	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
	}
	evmBlock, ok := wrapper.Block.(*Block)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to evm.Block", stateBlock)
	}
	evmBlock.SetStatus(choices.Accepted)
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

	if err := client.chain.BlockChain().ResetState(block); err != nil {
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
	return client.atomicTrie.ApplyToSharedMemory(block.NumberU64())
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
	if err := client.atomicTrie.MarkApplyToSharedMemoryCursor(client.lastAcceptedHeight); err != nil {
		return err
	}
	if err := client.acceptedBlockDB.Put(lastAcceptedKey, client.syncSummary.BlockHash[:]); err != nil {
		return err
	}
	if err := client.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return client.db.Commit()
}
