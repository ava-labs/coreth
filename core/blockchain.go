// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package core implements the Ethereum consensus protocol.
package core

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/misc/eip4844"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/lru"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/triedb"
)

// ====== If resolving merge conflicts ======
//
// All calls to metrics.NewRegistered*() for metrics also defined in libevm/core have been
// replaced either with:
//   - metrics.GetOrRegister*() to get a metric already registered in libevm/core, or register it
//     here otherwise
//   - [getOrOverrideAsRegisteredCounter] to get a metric already registered in libevm/core
//     only if it is a [metrics.Counter]. If it is not, the metric is unregistered and registered
//     as a [metrics.Counter] here.
//
// These replacements ensure the same metrics are shared between the two packages.
var (
	accountReadTimer   = getOrOverrideAsRegisteredCounter("chain/account/reads", nil)
	accountHashTimer   = getOrOverrideAsRegisteredCounter("chain/account/hashes", nil)
	accountUpdateTimer = getOrOverrideAsRegisteredCounter("chain/account/updates", nil)
	accountCommitTimer = getOrOverrideAsRegisteredCounter("chain/account/commits", nil)

	storageReadTimer   = getOrOverrideAsRegisteredCounter("chain/storage/reads", nil)
	storageHashTimer   = getOrOverrideAsRegisteredCounter("chain/storage/hashes", nil)
	storageUpdateTimer = getOrOverrideAsRegisteredCounter("chain/storage/updates", nil)
	storageCommitTimer = getOrOverrideAsRegisteredCounter("chain/storage/commits", nil)

	snapshotAccountReadTimer = getOrOverrideAsRegisteredCounter("chain/snapshot/account/reads", nil)
	snapshotStorageReadTimer = getOrOverrideAsRegisteredCounter("chain/snapshot/storage/reads", nil)
	snapshotCommitTimer      = getOrOverrideAsRegisteredCounter("chain/snapshot/commits", nil)

	triedbCommitTimer = getOrOverrideAsRegisteredCounter("chain/triedb/commits", nil)

	blockInsertTimer     = metrics.GetOrRegisterCounter("chain/block/inserts", nil)
	blockValidationTimer = metrics.GetOrRegisterCounter("chain/block/validations/state", nil)
	blockExecutionTimer  = metrics.GetOrRegisterCounter("chain/block/executions", nil)
	blockWriteTimer      = metrics.GetOrRegisterCounter("chain/block/writes", nil)

	errInvalidOldChain      = errors.New("invalid old chain")
	errInvalidNewChain      = errors.New("invalid new chain")
	______________________0 struct{} // For git diff text alignment only
)

const (
	bodyCacheLimit      = 256
	blockCacheLimit     = 256
	receiptsCacheLimit  = 32
	txLookupCacheLimit  = 1024
	maxFutureBlocks     = 256
	maxTimeFutureBlocks = 30
	TriesInMemory       = 128

	// BlockChainVersion ensures that an incompatible database forces a resync from scratch.
	//
	// Changelog:
	//
	// - Version 4
	//   The following incompatible database changes were added:
	//   * the `BlockNumber`, `TxHash`, `TxIndex`, `BlockHash` and `Index` fields of log are deleted
	//   * the `Bloom` field of receipt is deleted
	//   * the `BlockIndex` and `TxIndex` fields of txlookup are deleted
	// - Version 5
	//  The following incompatible database changes were added:
	//    * the `TxHash`, `GasCost`, and `ContractAddress` fields are no longer stored for a receipt
	//    * the `TxHash`, `GasCost`, and `ContractAddress` fields are computed by looking up the
	//      receipts' corresponding block
	// - Version 6
	//  The following incompatible database changes were added:
	//    * Transaction lookup information stores the corresponding block number instead of block hash
	// - Version 7
	//  The following incompatible database changes were added:
	//    * Use freezer as the ancient database to maintain all ancient data
	// - Version 8
	//  The following incompatible database changes were added:
	//    * New scheme for contract code in order to separate the codes and trie nodes
	BlockChainVersion uint64 = 8
)

// CacheConfig contains the configuration values for the trie database
// and state snapshot these are resident in a blockchain.
type CacheConfig struct {
	TrieCleanLimit      int           // Memory allowance (MB) to use for caching trie nodes in memory
	TrieDirtyLimit      int           // Memory limit (MB) at which to start flushing dirty trie nodes to disk
	SnapshotLimit       int           // Memory allowance (MB) to use for caching snapshot entries in memory
	Preimages           bool          // Whether to store preimage of trie key to the disk
	StateHistory        uint64        // Number of blocks from head whose state histories are reserved.
	StateScheme         string        // Scheme used to store ethereum states and merkle tree nodes on top
	__________________0 time.Duration // For git diff text alignment only

	SnapshotNoBuild bool // Whether the background generation is allowed
	SnapshotWait    bool // Wait for snapshot construction on startup. TODO(karalabe): This is a dirty hack for testing, nuke it

	AcceptedCacheSize               int     // Depth of accepted headers cache and accepted logs cache at the accepted tip
	AcceptorQueueLimit              int     // Blocks to queue before blocking during acceptance
	AllowMissingTries               bool    // Whether to allow an archive node to run with pruning enabled
	CommitInterval                  uint64  // Commit the trie every [CommitInterval] blocks.
	PopulateMissingTries            *uint64 // If non-nil, sets the starting height for re-generating historical tries.
	PopulateMissingTriesParallelism int     // Number of readers to use when trying to populate missing tries.
	Pruning                         bool    // Whether to disable trie write caching and GC altogether (archive node)
	SkipTxIndexing                  bool    // Whether to skip transaction indexing
	SnapshotDelayInit               bool    // Whether to initialize snapshots on startup or wait for external call (= StateSyncEnabled)
	SnapshotVerify                  bool    // Verify generated snapshots
	TransactionHistory              uint64  // Number of recent blocks for which to maintain transaction lookup indices
	TrieDirtyCommitTarget           int     // Memory limit (MB) to target for the dirties cache before invoking commit
	TriePrefetcherParallelism       int     // Max concurrent disk reads trie prefetcher should perform at once
}

// triedbConfig derives the configures for trie database.
func (c *CacheConfig) triedbConfig() *triedb.Config {
	config := &triedb.Config{Preimages: c.Preimages}
	if c.StateScheme == rawdb.HashScheme || c.StateScheme == "" {
		config.DBOverride = hashdb.Config{
			CleanCacheSize:                  c.TrieCleanLimit * 1024 * 1024,
			StatsPrefix:                     trieCleanCacheStatsNamespace,
			ReferenceRootAtomicallyOnUpdate: true,
		}.BackendConstructor
	}
	if c.StateScheme == rawdb.PathScheme {
		config.DBOverride = pathdb.Config{
			StateHistory:   c.StateHistory,
			CleanCacheSize: c.TrieCleanLimit * 1024 * 1024,
			DirtyCacheSize: c.TrieDirtyLimit * 1024 * 1024,
		}.BackendConstructor
	}
	return config
}

// defaultCacheConfig are the default caching values if none are specified by the
// user (also used during testing).
var defaultCacheConfig = &CacheConfig{
	TrieCleanLimit: 256,
	TrieDirtyLimit: 256,
	SnapshotLimit:  256,
	SnapshotWait:   true,
	StateScheme:    rawdb.HashScheme,

	AcceptedCacheSize:         32,
	TrieDirtyCommitTarget:     20, // 20% overhead in memory counting (this targets 16 MB)
	TriePrefetcherParallelism: 16,
	Pruning:                   true,
	CommitInterval:            4096,
	AcceptorQueueLimit:        64, // Provides 2 minutes of buffer (2s block target) for a commit delay
}

// DefaultCacheConfigWithScheme returns a deep copied default cache config with
// a provided trie node scheme.
func DefaultCacheConfigWithScheme(scheme string) *CacheConfig {
	config := *defaultCacheConfig
	config.StateScheme = scheme
	return &config
}

// txLookup is wrapper over transaction lookup along with the corresponding
// transaction object.
type txLookup struct {
	lookup      *rawdb.LegacyTxLookupEntry
	transaction *types.Transaction
}

// BlockChain represents the canonical chain given a database with a genesis
// block. The Blockchain manages chain imports, reverts, chain reorganisations.
//
// Importing blocks in to the block chain happens according to the set of rules
// defined by the two stage Validator. Processing of blocks is done using the
// Processor which processes the included transaction. The validation of the state
// is done in the second part of the Validator. Failing results in aborting of
// the import.
//
// The BlockChain also helps in returning blocks from **any** chain included
// in the database as well as blocks that represents the canonical chain. It's
// important to note that GetBlock can return any block and does not need to be
// included in the canonical one where as GetBlockByNumber always represents the
// canonical chain.
type BlockChain struct {
	chainConfig *params.ChainConfig // Chain & network configuration
	cacheConfig *CacheConfig        // Cache configuration for pruning

	db            ethdb.Database                   // Low level persistent database to store final content in
	snaps         *snapshot.Tree                   // Snapshot tree for fast trie leaf access
	triedb        *triedb.Database                 // The database handler for maintaining trie nodes.
	stateCache    state.Database                   // State database to reuse between imports (contains state cache)
	txIndexer     *txIndexer                       // Transaction indexer, might be nil if not enabled
	____________0 struct{ ____________0 struct{} } // For git diff text alignment only

	hc            *HeaderChain
	rmLogsFeed    event.Feed
	chainFeed     event.Feed
	chainSideFeed event.Feed
	chainHeadFeed event.Feed
	logsFeed      event.Feed
	blockProcFeed event.Feed
	scope         event.SubscriptionScope
	genesisBlock  *types.Block

	chainAcceptedFeed event.Feed
	logsAcceptedFeed  event.Feed
	txAcceptedFeed    event.Feed

	// This mutex synchronizes chain write operations.
	// Readers don't need to take it, they can just read the database.
	chainmu sync.RWMutex

	currentBlock      atomic.Pointer[types.Header] // Current head of the chain
	________________0 struct{}                     // for git diff text alignment only

	bodyCache     *lru.Cache[common.Hash, *types.Body]
	receiptsCache *lru.Cache[common.Hash, []*types.Receipt]
	blockCache    *lru.Cache[common.Hash, *types.Block]
	txLookupCache *lru.Cache[common.Hash, txLookup]

	wg            sync.WaitGroup
	quit          chan struct{} // shutdown signal, closed in Stop.
	stopping      atomic.Bool   // false if chain is running, true when stopped
	____________1 struct{}      // for git diff text alignment only

	engine     consensus.Engine
	validator  Validator // Block and state validator interface
	processor  Processor // Block transaction processor interface
	vmConfig   vm.Config
	_________0 struct{} // for git diff text alignment only

	lastAccepted *types.Block // Prevents reorgs past this height
	senderCacher *TxSenderCacher
	// [acceptorQueue] is a processing queue for the Acceptor. This is
	// different than [chainAcceptedFeed], which is sent an event after an accepted
	// block is processed (after each loop of the accepted worker). If there is a
	// clean shutdown, all items inserted into the [acceptorQueue] will be processed.
	acceptorQueue chan *types.Block
	// [acceptorClosingLock], and [acceptorClosed] are used
	// to synchronize the closing of the [acceptorQueue] channel.
	//
	// Because we can't check if a channel is closed without reading from it
	// (which we don't want to do as we may remove a processing block), we need
	// to use a second variable to ensure we don't close a closed channel.
	acceptorClosingLock sync.RWMutex
	acceptorClosed      bool
	// [acceptorWg] is used to wait for the acceptorQueue to clear. This is used
	// during shutdown and in tests.
	acceptorWg sync.WaitGroup
	// [acceptorTip] is the last block processed by the acceptor. This is
	// returned as the LastAcceptedBlock() to ensure clients get only fully
	// processed blocks. This may be equal to [lastAccepted].
	acceptorTip     *types.Block
	acceptorTipLock sync.Mutex
	// [flattenLock] prevents the [acceptor] from flattening snapshots while
	// a block is being verified.
	flattenLock sync.Mutex
	// [acceptedLogsCache] stores recently accepted logs to improve the performance of eth_getLogs.
	acceptedLogsCache FIFOCache[common.Hash, [][]*types.Log]
	// [txIndexTailLock] is used to synchronize the updating of the tx index tail.
	txIndexTailLock sync.Mutex
	badBlocks       *lru.Cache[common.Hash, *badBlock] // Cache for bad blocks
	stateManager    TrieWriter
}

// NewBlockChain returns a fully initialised block chain using information
// available in the database. It initialises the default Ethereum Validator
// and Processor.
func NewBlockChain(db ethdb.Database, cacheConfig *CacheConfig, genesis *Genesis, engine consensus.Engine, vmConfig vm.Config, lastAcceptedHash common.Hash, skipChainConfigCheckCompatible bool) (*BlockChain, error) {
	if cacheConfig == nil {
		return nil, errCacheConfigNotSpecified
	}
	// Open trie database with provided config
	triedb := triedb.NewDatabase(db, cacheConfig.triedbConfig())

	// Setup the genesis block, commit the provided genesis specification
	// to database if the genesis block is not present yet, or load the
	// stored one from database.
	// Note: In go-ethereum, the code rewinds the chain on an incompatible config upgrade.
	// We don't do this and expect the node operator to always update their node's configuration
	// before network upgrades take effect.
	chainConfig, _, err := SetupGenesisBlock(db, triedb, genesis, lastAcceptedHash, skipChainConfigCheckCompatible)
	if err != nil {
		return nil, err
	}
	log.Info("")
	log.Info(strings.Repeat("-", 153))
	for _, line := range strings.Split(chainConfig.Description(), "\n") {
		log.Info(line)
	}
	log.Info(strings.Repeat("-", 153))
	log.Info("")

	bc := &BlockChain{
		chainConfig:   chainConfig,
		cacheConfig:   cacheConfig,
		db:            db,
		triedb:        triedb,
		bodyCache:     lru.NewCache[common.Hash, *types.Body](bodyCacheLimit),
		receiptsCache: lru.NewCache[common.Hash, []*types.Receipt](receiptsCacheLimit),
		blockCache:    lru.NewCache[common.Hash, *types.Block](blockCacheLimit),
		txLookupCache: lru.NewCache[common.Hash, txLookup](txLookupCacheLimit),
		engine:        engine,
		vmConfig:      vmConfig,

		badBlocks:         lru.NewCache[common.Hash, *badBlock](badBlockLimit),
		senderCacher:      NewTxSenderCacher(runtime.NumCPU()),
		acceptorQueue:     make(chan *types.Block, cacheConfig.AcceptorQueueLimit),
		quit:              make(chan struct{}),
		acceptedLogsCache: NewFIFOCache[common.Hash, [][]*types.Log](cacheConfig.AcceptedCacheSize),
	}
	bc.stateCache = state.NewDatabaseWithNodeDB(bc.db, bc.triedb)
	bc.validator = NewBlockValidator(chainConfig, bc, engine)
	bc.processor = NewStateProcessor(chainConfig, bc, engine)

	bc.hc, err = NewHeaderChain(db, chainConfig, cacheConfig, engine)
	if err != nil {
		return nil, err
	}
	bc.genesisBlock = bc.GetBlockByNumber(0)
	if bc.genesisBlock == nil {
		return nil, ErrNoGenesis
	}

	bc.currentBlock.Store(nil)

	bc.stateManager = NewTrieWriter(bc.triedb, cacheConfig)

	// Load blockchain states from disk
	if err := bc.loadLastState(lastAcceptedHash); err != nil {
		return nil, err
	}

	// After loading the last state (and reprocessing if necessary), we are
	// guaranteed that [acceptorTip] is equal to [lastAccepted].
	//
	// It is critical to update this vaue before performing any state repairs so
	// that all accepted blocks can be considered.
	bc.acceptorTip = bc.lastAccepted

	// Make sure the state associated with the block is available, or log out
	// if there is no available state, waiting for state sync.
	head := bc.CurrentBlock()
	if !bc.HasState(head.Root) {
		return nil, fmt.Errorf("head state missing %d:%s", head.Number, head.Hash())
	}

	// Start future block processor.
	// Note:
	// - the comment above is only present for block git diffs with Geth code
	// - the error below is named setupErr for block git diffs with Geth code
	setupErr := setupBlockChainWithHead(bc, head)
	if setupErr != nil {
		return nil, fmt.Errorf("%w", err)
	}
	var txLookupLimit *uint64
	if bc.cacheConfig.TransactionHistory != 0 {
		txLookupLimit = new(uint64)
		*txLookupLimit = bc.cacheConfig.TransactionHistory
	}

	// Start tx indexer if it's enabled.
	if txLookupLimit != nil {
		bc.txIndexer = newTxIndexer(*txLookupLimit, bc)
	}
	return bc, nil
}

func (bc *BlockChain) loadGenesisState() error {
	genesis := bc.genesisBlock
	// Prepare the genesis block and reinitialise the chain
	batch := bc.db.NewBatch()
	rawdb.WriteBlock(batch, genesis)
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write genesis block", "err", err)
	}
	bc.writeHeadBlock(genesis)

	// Last update all in-memory chain markers
	bc.lastAccepted = genesis
	bc.currentBlock.Store(bc.genesisBlock.Header())
	bc.hc.SetGenesis(bc.genesisBlock.Header())
	bc.hc.SetCurrentHeader(bc.genesisBlock.Header())
	return nil
}

// Export writes the active chain to the given writer.
func (bc *BlockChain) Export(w io.Writer) error {
	return bc.ExportN(w, uint64(0), bc.CurrentBlock().Number.Uint64())
}

// ExportN writes a subset of the active chain to the given writer.
func (bc *BlockChain) ExportN(w io.Writer, first uint64, last uint64) error {
	if first > last {
		return fmt.Errorf("export failed: first (%d) is greater than last (%d)", first, last)
	}
	log.Info("Exporting batch of blocks", "count", last-first+1)

	var (
		parentHash common.Hash
		start      = time.Now()
		reported   = time.Now()
	)
	for nr := first; nr <= last; nr++ {
		block := bc.GetBlockByNumber(nr)
		if block == nil {
			return fmt.Errorf("export failed on #%d: not found", nr)
		}
		if nr > first && block.ParentHash() != parentHash {
			return errors.New("export failed: chain reorg during export")
		}
		parentHash = block.Hash()
		if err := block.EncodeRLP(w); err != nil {
			return err
		}
		if time.Since(reported) >= statsReportLimit {
			log.Info("Exporting blocks", "exported", block.NumberU64()-first, "elapsed", common.PrettyDuration(time.Since(start)))
			reported = time.Now()
		}
	}
	return nil
}

// writeHeadBlock injects a new head block into the current block chain. This method
// assumes that the block is indeed a true head. It will also reset the head
// header to this very same block if they are older
// or if they are on a different side chain.
//
// Note, this function assumes that the `mu` mutex is held!
func (bc *BlockChain) writeHeadBlock(block *types.Block) {
	// If the block is on a side chain or an unknown one, force other heads onto it too
	// Add the block to the canonical chain number scheme and mark as the head
	batch := bc.db.NewBatch()
	rawdb.WriteHeadHeaderHash(batch, block.Hash())
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(batch, block.Hash())

	// Flush the whole batch into the disk, exit the node if failed
	if err := batch.Write(); err != nil {
		log.Crit("Failed to update chain indexes and markers", "err", err)
	}
	// Update all in-memory chain markers in the last step
	bc.hc.SetCurrentHeader(block.Header())
	bc.currentBlock.Store(block.Header())
}

// stopWithoutSaving stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt. This method stops all running
// goroutines, but does not do all the post-stop work of persisting data.
// OBS! It is generally recommended to use the Stop method!
// This method has been exposed to allow tests to stop the blockchain while simulating
// a crash.
func (bc *BlockChain) stopWithoutSaving() {
	if !bc.stopping.CompareAndSwap(false, true) {
		return
	}
	// Signal shutdown tx indexer.
	if bc.txIndexer != nil {
		bc.txIndexer.close()
	}

	// Signal shutdown to all goroutines.
	close(bc.quit)
	// Wait for accepted feed to process all remaining items
	log.Info("Stopping Acceptor")
	start := time.Now()
	bc.stopAcceptor()
	log.Info("Acceptor queue drained", "t", time.Since(start))

	// Stop senderCacher's goroutines
	log.Info("Shutting down sender cacher")
	bc.senderCacher.Shutdown()

	// Unsubscribe all subscriptions registered from blockchain.
	log.Info("Closing scope")
	bc.scope.Close()

	// Waiting for background processes to complete
	log.Info("Waiting for background processes to complete")
	bc.wg.Wait()
}

// Stop stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt.
func (bc *BlockChain) Stop() {
	bc.stopWithoutSaving()

	// Ensure that the entirety of the state snapshot is journaled to disk.
	if bc.snaps != nil {
		bc.snaps.Release()
	}
	if bc.triedb.Scheme() == rawdb.PathScheme {
		// Ensure that the in-memory trie nodes are journaled to disk properly.
		if err := bc.triedb.Journal(bc.CurrentBlock().Root); err != nil {
			log.Info("Failed to journal in-memory trie nodes", "err", err)
		}
	}

	shutdownStateManager(bc.stateManager)

	// Close the trie database, release all the held resources as the last step.
	if err := bc.triedb.Close(); err != nil {
		log.Error("Failed to close trie database", "err", err)
	}
	log.Info("Blockchain stopped")
}

// writeKnownBlock updates the head block flag with a known block
// and introduces chain reorg if necessary.
func (bc *BlockChain) writeKnownBlock(block *types.Block) error {
	current := bc.CurrentBlock()
	if block.ParentHash() != current.Hash() {
		if err := bc.reorg(current, block); err != nil {
			return err
		}
	}
	bc.writeHeadBlock(block)
	return nil
}

// writeBlockWithState writes block, metadata and corresponding state data to the
// database.
// It expects the chain mutex to be held.
func (bc *BlockChain) writeBlockWithState(block *types.Block, parentRoot common.Hash, receipts []*types.Receipt, state *state.StateDB) error {
	// Irrelevant of the canonical status, write the block itself to the database.
	//
	// Note all the components of block(hash->number map, header, body, receipts)
	// should be written atomically. BlockBatch is used for containing all components.
	blockBatch := bc.db.NewBatch()
	rawdb.WriteBlock(blockBatch, block)
	rawdb.WriteReceipts(blockBatch, block.Hash(), block.NumberU64(), receipts)
	rawdb.WritePreimages(blockBatch, state.Preimages())
	if err := blockBatch.Write(); err != nil {
		log.Crit("Failed to write block into disk", "err", err)
	}
	// Commit all cached state changes into underlying memory database.
	_, err := bc.commitWithSnap(block, parentRoot, state)
	if err != nil {
		return err
	}
	// If node is running in path mode, skip explicit gc operation
	// which is unnecessary in this mode.
	if bc.triedb.Scheme() == rawdb.PathScheme {
		return nil
	}

	return insertTrie(bc, block)
}

// InsertChain attempts to insert the given batch of blocks in to the canonical
// chain or, otherwise, create a fork. If an error is returned it will return
// the index number of the failing block as well an error describing what went
// wrong. After insertion is done, all accumulated events will be fired.
func (bc *BlockChain) InsertChain(chain types.Blocks) (int, error) {
	// Sanity check that we have something meaningful to import
	if len(chain) == 0 {
		return 0, nil
	}
	bc.blockProcFeed.Send(true)
	defer bc.blockProcFeed.Send(false)

	// Do a sanity check that the provided chain is actually ordered and linked.
	for i := 1; i < len(chain); i++ {
		block, prev := chain[i], chain[i-1]
		if block.NumberU64() != prev.NumberU64()+1 || block.ParentHash() != prev.Hash() {
			log.Error("Non contiguous block insert",
				"number", block.Number(),
				"hash", block.Hash(),
				"parent", block.ParentHash(),
				"prevnumber", prev.Number(),
				"prevhash", prev.Hash(),
			)
			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x..], item %d is #%d [%x..] (parent [%x..])", i-1, prev.NumberU64(),
				prev.Hash().Bytes()[:4], i, block.NumberU64(), block.Hash().Bytes()[:4], block.ParentHash().Bytes()[:4])
		}
	}
	// Pre-checks passed, start the full block imports
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()
	return insertChain(bc, chain)
}

func (bc *BlockChain) insertBlock(block *types.Block, writes bool) error {
	start, substart, parent, xerr := insertBlockBeginning(bc, block, writes)
	if xerr != nil {
		// the error is named `xerr` and this comment is placed here
		// for git to clearly distinguish this new block from geth original code.
		return xerr
	}

	// Instantiate the statedb to use for processing transactions
	//
	// NOTE: Flattening a snapshot during block execution requires fetching state
	// entries directly from the trie (much slower).
	bc.flattenLock.Lock()
	defer bc.flattenLock.Unlock()
	statedb, err := state.New(parent.Root, bc.stateCache, bc.snaps)
	if err != nil {
		return err
	}
	blockStateInitTimer.Inc(time.Since(substart).Milliseconds())

	// Enable prefetching to pull in trie node paths while processing transactions
	statedb.StartPrefetcher("chain", state.WithConcurrentWorkers(bc.cacheConfig.TriePrefetcherParallelism))
	defer statedb.StopPrefetcher()

	// Process block using the parent state as reference point
	pstart := time.Now()
	receipts, logs, usedGas, err := bc.processor.Process(block, parent, statedb, bc.vmConfig)

	if serr := statedb.Error(); serr != nil {
		log.Error("statedb error encountered", "err", serr, "number", block.Number(), "hash", block.Hash())
	}

	if err != nil {
		bc.reportBlock(block, receipts, err)
		return err
	}
	ptime := time.Since(pstart)

	vstart := time.Now()
	if err := bc.validator.ValidateState(block, statedb, receipts, usedGas); err != nil {
		bc.reportBlock(block, receipts, err)
		return err
	}
	vtime := time.Since(vstart)

	// Update the metrics touched during block processing and validation
	accountReadTimer.Inc(statedb.AccountReads.Milliseconds())                  // Account reads are complete(in processing)
	storageReadTimer.Inc(statedb.StorageReads.Milliseconds())                  // Storage reads are complete(in processing)
	snapshotAccountReadTimer.Inc(statedb.SnapshotAccountReads.Milliseconds())  // Account reads are complete(in processing)
	snapshotStorageReadTimer.Inc(statedb.SnapshotStorageReads.Milliseconds())  // Storage reads are complete(in processing)
	accountUpdateTimer.Inc(statedb.AccountUpdates.Milliseconds())              // Account updates are complete(in validation)
	storageUpdateTimer.Inc(statedb.StorageUpdates.Milliseconds())              // Storage updates are complete(in validation)
	accountHashTimer.Inc(statedb.AccountHashes.Milliseconds())                 // Account hashes are complete(in validation)
	storageHashTimer.Inc(statedb.StorageHashes.Milliseconds())                 // Storage hashes are complete(in validation)
	triehash := statedb.AccountHashes + statedb.StorageHashes                  // The time spent on tries hashing
	trieUpdate := statedb.AccountUpdates + statedb.StorageUpdates              // The time spent on tries update
	trieRead := statedb.SnapshotAccountReads + statedb.AccountReads            // The time spent on account read
	trieRead += statedb.SnapshotStorageReads + statedb.StorageReads            // The time spent on storage read
	blockExecutionTimer.Inc((ptime - trieRead).Milliseconds())                 // The time spent on EVM processing
	blockValidationTimer.Inc((vtime - (triehash + trieUpdate)).Milliseconds()) // The time spent on block validation
	blockTrieOpsTimer.Inc((triehash + trieUpdate + trieRead).Milliseconds())   // The time spent on trie operations

	// If [writes] are disabled, skip [writeBlockWithState] so that we do not write the block
	// or the state trie to disk.
	// Note: in pruning mode, this prevents us from generating a reference to the state root.
	if !writes {
		return nil
	}

	// Write the block to the chain and get the status.
	var (
		wstart = time.Now()
	)
	// writeBlockWithState (called within writeBlockAndSethead) creates a reference that
	// will be cleaned up in Accept/Reject so we need to ensure an error cannot occur
	// later in verification, since that would cause the referenced root to never be dereferenced.
	err = bc.writeBlockAndSetHead(block, parent.Root, receipts, logs, statedb)
	if err != nil {
		return err
	}
	// Update the metrics touched during block commit
	accountCommitTimer.Inc(statedb.AccountCommits.Milliseconds())   // Account commits are complete, we can mark them
	storageCommitTimer.Inc(statedb.StorageCommits.Milliseconds())   // Storage commits are complete, we can mark them
	snapshotCommitTimer.Inc(statedb.SnapshotCommits.Milliseconds()) // Snapshot commits are complete, we can mark them
	triedbCommitTimer.Inc(statedb.TrieDBCommits.Milliseconds())     // Trie database commits are complete, we can mark them

	blockWriteTimer.Inc((time.Since(wstart) - statedb.AccountCommits - statedb.StorageCommits - statedb.SnapshotCommits - statedb.TrieDBCommits).Milliseconds())
	blockInsertTimer.Inc(time.Since(start).Milliseconds())

	log.Debug("Inserted new block", "number", block.Number(), "hash", block.Hash(),
		"parentHash", block.ParentHash(),
		"uncles", len(block.Uncles()), "txs", len(block.Transactions()), "gas", block.GasUsed(),
		"elapsed", common.PrettyDuration(time.Since(start)),
		"root", block.Root(),
		"baseFeePerGas", block.BaseFee(), "blockGasCost", customtypes.BlockGasCost(block),
	)

	processedBlockGasUsedCounter.Inc(int64(block.GasUsed()))
	processedTxsCounter.Inc(int64(block.Transactions().Len()))
	processedLogsCounter.Inc(int64(len(logs)))
	blockInsertCount.Inc(1)
	return nil
}

// collectLogs collects the logs that were generated or removed during
// the processing of a block. These logs are later announced as deleted or reborn.
func (bc *BlockChain) collectLogs(b *types.Block, removed bool) []*types.Log {
	unflattenedLogs := bc.collectUnflattenedLogs(b, removed)
	return customtypes.FlattenLogs(unflattenedLogs)
}

// collectUnflattenedLogs collects the logs that were generated or removed during
// the processing of a block.
func (bc *BlockChain) collectUnflattenedLogs(b *types.Block, removed bool) [][]*types.Log {
	var blobGasPrice *big.Int
	excessBlobGas := b.ExcessBlobGas()
	if excessBlobGas != nil {
		blobGasPrice = eip4844.CalcBlobFee(*excessBlobGas)
	}
	receipts := rawdb.ReadRawReceipts(bc.db, b.Hash(), b.NumberU64())
	if err := receipts.DeriveFields(bc.chainConfig, b.Hash(), b.NumberU64(), b.Time(), b.BaseFee(), blobGasPrice, b.Transactions()); err != nil {
		log.Error("Failed to derive block receipts fields", "hash", b.Hash(), "number", b.NumberU64(), "err", err)
	}

	// Note: gross but this needs to be initialized here because returning nil will be treated specially as an incorrect
	// error case downstream.
	logs := make([][]*types.Log, len(receipts))
	for i, receipt := range receipts {
		for _, log := range receipt.Logs {
			if removed {
				log.Removed = true
			}
			logs[i] = append(logs[i], log)
		}
	}
	return logs
}

// reorg takes two blocks, an old chain and a new chain and will reconstruct the
// blocks and inserts them to be part of the new canonical chain and accumulates
// potential missing transactions and post an event about them.
func (bc *BlockChain) reorg(oldHead *types.Header, newHead *types.Block) error {
	var (
		newChain    types.Blocks
		oldChain    types.Blocks
		commonBlock *types.Block
	)
	oldBlock := bc.GetBlock(oldHead.Hash(), oldHead.Number.Uint64())
	if oldBlock == nil {
		return errors.New("current head block missing")
	}
	newBlock := newHead

	// Reduce the longer chain to the same number as the shorter one
	if oldBlock.NumberU64() > newBlock.NumberU64() {
		// Old chain is longer, gather all transactions and logs as deleted ones
		for ; oldBlock != nil && oldBlock.NumberU64() != newBlock.NumberU64(); oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1) {
			oldChain = append(oldChain, oldBlock)
		}
	} else {
		// New chain is longer, stash all blocks away for subsequent insertion
		for ; newBlock != nil && newBlock.NumberU64() != oldBlock.NumberU64(); newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1) {
			newChain = append(newChain, newBlock)
		}
	}
	if oldBlock == nil {
		return errInvalidOldChain
	}
	if newBlock == nil {
		return errInvalidNewChain
	}
	// Both sides of the reorg are at the same number, reduce both until the common
	// ancestor is found
	for {
		// If the common ancestor was found, bail out
		if oldBlock.Hash() == newBlock.Hash() {
			commonBlock = oldBlock
			break
		}
		// Remove an old block as well as stash away a new block
		oldChain = append(oldChain, oldBlock)
		newChain = append(newChain, newBlock)

		// Step back with both chains
		oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1)
		if oldBlock == nil {
			return errInvalidOldChain
		}
		newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1)
		if newBlock == nil {
			return errInvalidNewChain
		}
	}

	// If the commonBlock is less than the last accepted height, we return an error
	// because performing a reorg would mean removing an accepted block from the
	// canonical chain.
	if commonBlock.NumberU64() < bc.lastAccepted.NumberU64() {
		return fmt.Errorf("cannot orphan finalized block at height: %d to common block at height: %d", bc.lastAccepted.NumberU64(), commonBlock.NumberU64())
	}

	// Ensure the user sees large reorgs
	if len(oldChain) > 0 && len(newChain) > 0 {
		logFn := log.Info
		msg := "Resetting chain preference"
		if len(oldChain) > 63 {
			msg = "Large chain preference change detected"
			logFn = log.Warn
		}
		logFn(msg, "number", commonBlock.Number(), "hash", commonBlock.Hash(),
			"drop", len(oldChain), "dropfrom", oldChain[0].Hash(), "add", len(newChain), "addfrom", newChain[0].Hash())
	} else {
		log.Debug("Preference change (rewind to ancestor) occurred", "oldnum", oldHead.Number, "oldhash", oldHead.Hash(), "newnum", newHead.Number(), "newhash", newHead.Hash())
	}
	// Reset the tx lookup cache in case to clear stale txlookups.
	// This is done before writing any new chain data to avoid the
	// weird scenario that canonical chain is changed while the
	// stale lookups are still cached.
	bc.txLookupCache.Purge()

	// Insert the new chain(except the head block(reverse order)),
	// taking care of the proper incremental order.
	for i := len(newChain) - 1; i >= 1; i-- {
		// Insert the block in the canonical way, re-writing history
		bc.writeHeadBlock(newChain[i])
	}

	// Delete useless indexes right now which includes the non-canonical
	// transaction indexes, canonical chain indexes which above the head.
	var (
		indexesBatch = bc.db.NewBatch()
	)

	// Use the height of [newHead] to determine which canonical hashes to remove
	// in case the new chain is shorter than the old chain, in which case
	// there may be hashes set on the canonical chain that were invalidated
	// but not yet overwritten by the re-org.
	number := newHead.NumberU64()
	for i := number + 1; ; i++ {
		hash := rawdb.ReadCanonicalHash(bc.db, i)
		if hash == (common.Hash{}) {
			break
		}
		rawdb.DeleteCanonicalHash(indexesBatch, i)
	}
	if err := indexesBatch.Write(); err != nil {
		log.Crit("Failed to delete useless indexes", "err", err)
	}

	// Send out events for logs from the old canon chain, and 'reborn'
	// logs from the new canon chain. The number of logs can be very
	// high, so the events are sent in batches of size around 512.

	// Deleted logs + blocks:
	var deletedLogs []*types.Log
	for i := len(oldChain) - 1; i >= 0; i-- {
		// Also send event for blocks removed from the canon chain.
		bc.chainSideFeed.Send(ChainSideEvent{Block: oldChain[i]})

		// Collect deleted logs for notification
		if logs := bc.collectLogs(oldChain[i], true); len(logs) > 0 {
			deletedLogs = append(deletedLogs, logs...)
		}
		if len(deletedLogs) > 512 {
			bc.rmLogsFeed.Send(RemovedLogsEvent{deletedLogs})
			deletedLogs = nil
		}
	}
	if len(deletedLogs) > 0 {
		bc.rmLogsFeed.Send(RemovedLogsEvent{deletedLogs})
	}

	// New logs:
	var rebirthLogs []*types.Log
	for i := len(newChain) - 1; i >= 1; i-- {
		if logs := bc.collectLogs(newChain[i], false); len(logs) > 0 {
			rebirthLogs = append(rebirthLogs, logs...)
		}
		if len(rebirthLogs) > 512 {
			bc.logsFeed.Send(rebirthLogs)
			rebirthLogs = nil
		}
	}
	if len(rebirthLogs) > 0 {
		bc.logsFeed.Send(rebirthLogs)
	}
	return nil
}
