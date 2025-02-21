// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"sync"

	// "time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	ethstatesync "github.com/ava-labs/coreth/plugin/evm/statesync"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

const (
	// State sync fetches [parentsToGet] parents of the block it syncs to.
	// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
	parentsToGet  = 256
	pivotInterval = 128
	bufferSize    = 6 * 60 * 30 // 2 * pivotInterval // extra space to be careful
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
}

type stateSyncerClient struct {
	*stateSyncClientConfig

	resumableSummary message.SyncSummary

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State Sync results
	syncSummary  message.SyncSummary
	stateSyncErr error

	// Testing dynamic sync
	syncing utils.Atomic[bool]
	dl      *downloader
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

	// Methods to try to enable dynamic state sync
	AsyncReceive() bool
	QueueVerifyBlock(*Block) error
	QueueAcceptBlock(*Block) error
	QueueRejectBlock(*Block) error
}

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	Start(ctx context.Context) error
	Done() <-chan error
}

// AsyncReceive returns true if the client is ready to receive a message from the engine
// Should return true if syncing and useUpstream is true, i.e. currently dynamicaling syncing
func (client *stateSyncerClient) AsyncReceive() bool {
	return client.useUpstream && client.syncing.Get() && client.dl != nil && !client.dl.execQueue.Closed()
}

func (client *stateSyncerClient) QueueVerifyBlock(b *Block) error {
	if !client.AsyncReceive() {
		return fmt.Errorf("cannot queue block when not using upstream syncing")
	}

	verify := func(a *Block) error {
		return a.VerifyDuringSync(context.TODO()) // is this right?
	}
	return client.dl.execQueue.Insert(&queueElement{b, verify})
}

func (client *stateSyncerClient) QueueAcceptBlock(b *Block) error {
	if !client.AsyncReceive() {
		return fmt.Errorf("cannot queue block when not using upstream syncing")
	}

	accept := func(a *Block) error {
		return a.AcceptDuringSync(context.TODO()) // yeah gotta fix this too
	}

	if err := client.dl.execQueue.Insert(&queueElement{b, accept}); err != nil {
		return err
	}

	// If the block is the pivot, signal the state syncer to start
	client.dl.pivotLock.Lock()
	if b.Height() >= client.dl.pivotBlock.Height()+pivotInterval {
		log.Info("Setting new pivot block", "hash", b.ID(), "height", b.Height(), "timestamp", b.Timestamp())
		client.dl.pivotBlock = b
		client.dl.newPivot <- b
	}
	client.dl.pivotLock.Unlock()

	return nil
}

func (client *stateSyncerClient) QueueRejectBlock(b *Block) error {
	if !client.AsyncReceive() {
		return fmt.Errorf("cannot queue block when not using upstream syncing")
	}

	reject := func(a *Block) error {
		return a.RejectDuringSync(context.TODO()) // yeah gotta fix this too
	}

	if err := client.dl.execQueue.Insert(&queueElement{b, reject}); err != nil {
		return err
	}

	// If the block is the pivot, signal the state syncer to start
	client.dl.pivotLock.RLock()
	defer client.dl.pivotLock.RUnlock()
	if b.Height() == client.dl.pivotBlock.Height() {
		return fmt.Errorf("cannot reject pivot block")
	}

	return nil
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
	if err := client.syncBlocks(ctx, client.syncSummary.BlockHash, client.syncSummary.BlockNumber, parentsToGet); err != nil {
		return err
	}

	// Sync the EVM trie and then the atomic trie. These steps could be done
	// in parallel or in the opposite order in the static case
	// For dynamic, much simpler to do atomic trie first

	if err := client.syncAtomicTrie(ctx); err != nil {
		return err
	}

	return client.syncStateTrie(ctx)
}

func convertReadOnlyToBidirectional[T any](readOnly <-chan T) chan T {
	bidirectional := make(chan T)

	go func() {
		defer close(bidirectional)
		for value := range readOnly {
			bidirectional <- value
		}
	}()

	return bidirectional
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
		// Must first find first pivot block to signal bootstrapper
		stateBlock, err := client.state.GetBlock(context.TODO(), ids.ID(client.syncSummary.BlockHash))
		if err != nil {
			return block.StateSyncDynamic, fmt.Errorf("could not get block by hash from client state: %s", client.syncSummary.BlockHash)
		}
		wrapper, ok := stateBlock.(*chain.BlockWrapper)
		if !ok {
			return block.StateSyncDynamic, fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
		}
		evmBlock, ok := wrapper.Block.(*Block)
		if !ok {
			return block.StateSyncDynamic, fmt.Errorf("could not convert block(%T) to evm.Block", stateBlock)
		}

		b := evmBlock.ethBlock
		parentHeight := b.NumberU64() - 1
		parentHash := b.ParentHash()
		client.chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

		if err := client.updateVMMarkers(); err != nil {
			return block.StateSyncDynamic, fmt.Errorf("error updating vm markers, height=%d, hash=%s, err=%w", b.NumberU64(), b.Hash(), err)
		}

		if err := client.state.SetLastAcceptedBlock(evmBlock); err != nil {
			return block.StateSyncDynamic, err
		}

		if err := client.atomicBackend.ApplyToSharedMemory(b.NumberU64()); err != nil {
			return block.StateSyncDynamic, err
		}

		// Set downloader using pivot
		client.dl = newDownloader(client.chaindb, evmBlock)

		log.Info("Set LastAcceptedBlock to first pivot with height", evmBlock.Height(), "timestamp", evmBlock.Timestamp())
	}
	go func() {
		defer client.wg.Done()
		defer cancel()

		client.syncing.Set(true)
		if err := client.stateSync(ctx); err != nil {
			client.stateSyncErr = err
		} else {
			client.stateSyncErr = client.finishSync()
		}
		// notify engine regardless of whether err == nil,
		// this error will be propagated to the engine when it calls
		// vm.SetState(snow.Bootstrapping)
		log.Info("stateSync completed, notifying engine", "err", client.stateSyncErr)
		client.toEngine <- commonEng.StateSyncDone
		client.syncing.Set(false)
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
	return err
}

func (client *stateSyncerClient) syncStateTrie(ctx context.Context) error {
	log.Info("state sync: sync starting", "root", client.syncSummary.BlockRoot)

	if client.useUpstream {
		log.Warn("Using upstream state syncer (untested)")
		return client.upstreamSyncStateTrie(ctx)
	} else {
		evmSyncer, err := statesync.NewStateSyncer(&statesync.StateSyncerConfig{
			Client:                   client.client,
			Root:                     client.syncSummary.BlockRoot,
			BatchSize:                ethdb.IdealBatchSize,
			DB:                       client.chaindb,
			MaxOutstandingCodeHashes: statesync.DefaultMaxOutstandingCodeHashes,
			NumCodeFetchingWorkers:   statesync.DefaultNumCodeFetchingWorkers,
			RequestSize:              client.stateSyncRequestSize,
		})
		if err != nil {
			return err
		}
		if err := evmSyncer.Start(ctx); err != nil {
			return err
		}
		err = <-evmSyncer.Done()
		log.Info("state sync: sync finished", "root", client.syncSummary.BlockRoot, "err", err)
		return err
	}
}

func (client *stateSyncerClient) Shutdown() error {
	if client.cancel != nil {
		client.cancel()
	}
	client.wg.Wait() // wait for the background goroutine to exit
	return nil
}

// finishSync is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (client *stateSyncerClient) finishSync() error {
	stateBlock, err := client.state.GetBlock(context.TODO(), ids.ID(client.syncSummary.BlockHash))
	if err != nil {
		return fmt.Errorf("could not get block by hash from client state: %s", client.syncSummary.BlockHash)
	}

	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
	}
	evmBlock, ok := wrapper.Block.(*Block)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to evm.Block", stateBlock)
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
	return client.atomicBackend.ApplyToSharedMemory(block.NumberU64())
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

// upstreamSyncStateTrie syncs the state trie using the upstream state syncer
func (client *stateSyncerClient) upstreamSyncStateTrie(ctx context.Context) error {
	p2pClient := client.network.NewClient(ethstatesync.ProtocolID)
	if len(client.stateSyncNodes) > 0 {
		for _, nodeID := range client.stateSyncNodes {
			client.dl.SnapSyncer.Register(ethstatesync.NewOutboundPeer(nodeID, client.dl.SnapSyncer, p2pClient))
		}
	} else {
		client.network.AddConnector(ethstatesync.NewConnector(client.dl.SnapSyncer, p2pClient))
	}

	if err := client.dl.SnapSync(); err != nil {
		return err
	}
	log.Info("Upstream state syncer completed, moving to catch-up")

	// Now that we have synced the state trie to static pivot, Verify and Accept all pending blocks
	// Incoming blocks are still appended to buffer until emptied
	err := client.dl.execQueue.Flush(nil, true)
	return err
}

func (e queueElement) ExitQueue() error {
	return e.exec(e.block)
}

type queueElement struct {
	block *Block
	exec  func(*Block) error
}

var _ ethstatesync.Executable = &queueElement{}

type downloader struct {
	pivotLock  sync.RWMutex
	pivotBlock *Block
	execQueue  *ethstatesync.Queue[queueElement]
	SnapSyncer *snap.Syncer

	stateSyncStart chan *stateSync
	newPivot       chan *Block
	quitCh         chan struct{} // Quit channel to signal termination
	// quitLock       sync.Mutex    // Lock to prevent double closes
}

func newDownloader(chaindb ethdb.Database, firstPivot *Block) *downloader {
	compare := func(a, b *queueElement) int {
		return int(a.block.Height()) - int(b.block.Height())
	}

	d := &downloader{
		pivotBlock:     firstPivot,
		execQueue:      ethstatesync.NewQueue[queueElement](bufferSize, compare),
		SnapSyncer:     snap.NewSyncer(chaindb, rawdb.HashScheme),
		stateSyncStart: make(chan *stateSync),
		quitCh:         make(chan struct{}),
		newPivot:       make(chan *Block),
	}

	go d.stateFetcher()

	return d
}

// stateFetcher manages the active state sync and accepts requests
// on its behalf.
func (d *downloader) stateFetcher() {
	for {
		select {
		case s := <-d.stateSyncStart:
			for next := s; next != nil; {
				next = d.runStateSync(next)
			}
		case <-d.quitCh:
			return
		}
	}
}

// processSnapSyncContent takes fetch results from the queue and writes them to the
// database. It also controls the synchronisation of state nodes of the pivot block.
func (d *downloader) SnapSync() error {
	// Start syncing state of the reported head block. This should get us most of
	// the state of the pivot block.
	d.pivotLock.RLock()
	sync := d.syncState(d.pivotBlock.ethBlock.Root())
	d.pivotLock.RUnlock()

	defer func() {
		// The `sync` object is replaced every time the pivot moves. We need to
		// defer close the very last active one, hence the lazy evaluation vs.
		// calling defer sync.Cancel() !!!
		sync.Cancel()
	}()

	for {
		select {
		// If stateSync is ended, clear queue and return
		// If err, just return so we can see it
		case <-sync.done:
			if sync.err != nil {
				return sync.err
			}
			return d.execQueue.Flush(nil, true) // might need to provide a cancle channel
		case np := <-d.newPivot:
			// If a new pivot block is found, cancel the current state sync and
			// start a new one.
			sync.Cancel()
			d.execQueue.Flush(&queueElement{np, nil}, false)
			sync = d.syncState(np.ethBlock.Root())
		}
	}
}

// syncState starts downloading state with the given root hash.
func (d *downloader) syncState(root common.Hash) *stateSync {
	// Create the state sync
	s := newStateSync(d, root)
	select {
	case d.stateSyncStart <- s:
		// If we tell the statesync to restart with a new root, we also need
		// to wait for it to actually also start -- when old requests have timed
		// out or been delivered
		<-s.started
	case <-d.quitCh:
		s.err = fmt.Errorf("errCancelStateFetch") //errCancelStateFetch
		close(s.done)
	}
	return s
}

// runStateSync runs a state synchronisation until it completes or another root
// hash is requested to be switched over to.
func (d *downloader) runStateSync(s *stateSync) *stateSync {
	log.Trace("State sync starting", "root", s.root)

	go s.run()
	defer s.Cancel()

	for {
		select {
		case next := <-d.stateSyncStart:
			return next

		case <-s.done:
			return nil
		}
	}
}

// stateSync schedules requests for downloading a particular state trie defined
// by a given state root.
type stateSync struct {
	d    *downloader // Downloader instance to access and manage current peerset
	root common.Hash // State root currently being synced

	started    chan struct{} // Started is signalled once the sync loop starts
	cancel     chan struct{} // Channel to signal a termination request
	cancelOnce sync.Once     // Ensures cancel only ever gets called once
	done       chan struct{} // Channel to signal termination completion
	err        error         // Any error hit during sync (set before completion)
}

// newStateSync creates a new state trie download scheduler. This method does not
// yet start the sync. The user needs to call run to initiate.
func newStateSync(d *downloader, root common.Hash) *stateSync {
	return &stateSync{
		d:       d,
		root:    root,
		cancel:  make(chan struct{}),
		done:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

// run starts the task assignment and response processing loop, blocking until
// it finishes, and finally notifying any goroutines waiting for the loop to
// finish.
func (s *stateSync) run() {
	close(s.started)
	log.Info("Starting new sync")
	s.err = s.d.SnapSyncer.Sync(s.root, s.cancel)
	close(s.done)
}

// Wait blocks until the sync is done or canceled.
func (s *stateSync) Wait() error {
	<-s.done
	return s.err
}

// Cancel cancels the sync and waits until it has shut down.
func (s *stateSync) Cancel() error {
	s.cancelOnce.Do(func() {
		close(s.cancel)
	})
	return s.Wait()
}
