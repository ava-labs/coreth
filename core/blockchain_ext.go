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
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/misc/eip4844"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/internal/version"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

var (
	blockInsertCount            = metrics.NewRegisteredCounter("chain/block/inserts/count", nil)
	blockContentValidationTimer = metrics.NewRegisteredCounter("chain/block/validations/content", nil)
	blockStateInitTimer         = metrics.NewRegisteredCounter("chain/block/inits/state", nil)
	blockTrieOpsTimer           = metrics.NewRegisteredCounter("chain/block/trie", nil)

	acceptorQueueGauge           = metrics.NewRegisteredGauge("chain/acceptor/queue/size", nil)
	acceptorWorkTimer            = metrics.NewRegisteredCounter("chain/acceptor/work", nil)
	acceptorWorkCount            = metrics.NewRegisteredCounter("chain/acceptor/work/count", nil)
	processedBlockGasUsedCounter = metrics.NewRegisteredCounter("chain/block/gas/used/processed", nil)
	acceptedBlockGasUsedCounter  = metrics.NewRegisteredCounter("chain/block/gas/used/accepted", nil)
	badBlockCounter              = metrics.NewRegisteredCounter("chain/block/bad/count", nil)

	txUnindexTimer      = metrics.NewRegisteredCounter("chain/txs/unindex", nil)
	acceptedTxsCounter  = metrics.NewRegisteredCounter("chain/txs/accepted", nil)
	processedTxsCounter = metrics.NewRegisteredCounter("chain/txs/processed", nil)

	acceptedLogsCounter  = metrics.NewRegisteredCounter("chain/logs/accepted", nil)
	processedLogsCounter = metrics.NewRegisteredCounter("chain/logs/processed", nil)

	ErrRefuseToCorruptArchiver = errors.New("node has operated with pruning disabled, shutting down to prevent missing tries")

	errFutureBlockUnsupported  = errors.New("future block insertion not supported")
	errCacheConfigNotSpecified = errors.New("must specify cache config")
)

const (
	badBlockLimit = 10

	// trieCleanCacheStatsNamespace is the namespace to surface stats from the trie
	// clean cache's underlying fastcache.
	trieCleanCacheStatsNamespace = "hashdb/memcache/clean/fastcache"
)

// CacheConfig contains the configuration values for the trie database
// and state snapshot these are resident in a blockchain.
type CacheConfig struct {
	TrieCleanLimit                  int     // Memory allowance (MB) to use for caching trie nodes in memory
	TrieDirtyLimit                  int     // Memory limit (MB) at which to block on insert and force a flush of dirty trie nodes to disk
	TrieDirtyCommitTarget           int     // Memory limit (MB) to target for the dirties cache before invoking commit
	TriePrefetcherParallelism       int     // Max concurrent disk reads trie prefetcher should perform at once
	CommitInterval                  uint64  // Commit the trie every [CommitInterval] blocks.
	Pruning                         bool    // Whether to disable trie write caching and GC altogether (archive node)
	AcceptorQueueLimit              int     // Blocks to queue before blocking during acceptance
	PopulateMissingTries            *uint64 // If non-nil, sets the starting height for re-generating historical tries.
	PopulateMissingTriesParallelism int     // Number of readers to use when trying to populate missing tries.
	AllowMissingTries               bool    // Whether to allow an archive node to run with pruning enabled
	SnapshotDelayInit               bool    // Whether to initialize snapshots on startup or wait for external call (= StateSyncEnabled)
	SnapshotLimit                   int     // Memory allowance (MB) to use for caching snapshot entries in memory
	SnapshotVerify                  bool    // Verify generated snapshots
	Preimages                       bool    // Whether to store preimage of trie key to the disk
	AcceptedCacheSize               int     // Depth of accepted headers cache and accepted logs cache at the accepted tip
	TransactionHistory              uint64  // Number of recent blocks for which to maintain transaction lookup indices
	SkipTxIndexing                  bool    // Whether to skip transaction indexing
	StateHistory                    uint64  // Number of blocks from head whose state histories are reserved.
	StateScheme                     string  // Scheme used to store ethereum states and merkle tree nodes on top

	SnapshotNoBuild bool // Whether the background generation is allowed
	SnapshotWait    bool // Wait for snapshot construction on startup. TODO(karalabe): This is a dirty hack for testing, nuke it
}

// triedbConfig derives the configures for trie database.
func (c *CacheConfig) triedbConfig() *triedb.Config {
	config := &triedb.Config{Preimages: c.Preimages}
	if c.StateScheme == rawdb.HashScheme || c.StateScheme == "" {
		config.HashDB = &hashdb.Config{
			CleanCacheSize: c.TrieCleanLimit * 1024 * 1024,
			StatsPrefix:    trieCleanCacheStatsNamespace,
			ReferenceRoot:  true, // Automatically reference root nodes when an update is made
		}
	}
	if c.StateScheme == rawdb.PathScheme {
		config.PathDB = &pathdb.Config{
			StateHistory:   c.StateHistory,
			CleanCacheSize: c.TrieCleanLimit * 1024 * 1024,
			DirtyCacheSize: c.TrieDirtyLimit * 1024 * 1024,
		}
	}
	return config
}

func (c *CacheConfig) cacheConfig() *cacheConfig {
	return &cacheConfig{
		TrieCleanLimit:      c.TrieCleanLimit,
		TrieCleanNoPrefetch: defaultCacheConfig.TrieCleanNoPrefetch,
		TrieDirtyLimit:      c.TrieDirtyLimit,
		TrieDirtyDisabled:   defaultCacheConfig.TrieDirtyDisabled,
		TrieTimeLimit:       defaultCacheConfig.TrieTimeLimit,

		Preimages:    c.Preimages,
		StateHistory: c.StateHistory,
		StateScheme:  c.StateScheme,

		SnapshotNoBuild: c.SnapshotNoBuild,
		SnapshotLimit:   c.SnapshotLimit,
		SnapshotWait:    c.SnapshotWait,

		// ADDED
		SnapshotVerify:          c.SnapshotVerify,
		SnapshotDelayInit:       true, // Always initialize snapshots in blockchain_ext.go if they are enabled
		TrieRefCountingDisabled: true, // Always disable trie ref counting in blockchain_ext.go
	}
}

// DefaultCacheConfig are the default caching values if none are specified by the
// user (also used during testing).
var DefaultCacheConfig = &CacheConfig{
	TrieCleanLimit:            256,
	TrieDirtyLimit:            256,
	TrieDirtyCommitTarget:     20, // 20% overhead in memory counting (this targets 16 MB)
	TriePrefetcherParallelism: 16,
	Pruning:                   true,
	CommitInterval:            4096,
	AcceptorQueueLimit:        64, // Provides 2 minutes of buffer (2s block target) for a commit delay
	SnapshotLimit:             256,
	AcceptedCacheSize:         32,
	SnapshotWait:              true,
	StateScheme:               rawdb.HashScheme,
}

// DefaultCacheConfigWithScheme returns a deep copied default cache config with
// a provided trie node scheme.
func DefaultCacheConfigWithScheme(scheme string) *CacheConfig {
	config := *DefaultCacheConfig
	config.StateScheme = scheme
	return &config
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
	*blockChain

	chainConfig *params.ChainConfig // Chain & network configuration
	cacheConfig *CacheConfig        // Cache configuration for pruning

	db           ethdb.Database   // Low level persistent database to store final content in
	triedb       *triedb.Database // The database handler for maintaining trie nodes.
	txIndexer    *txIndexer       // Transaction indexer, might be nil if not enabled
	stateManager TrieWriter

	chainAcceptedFeed event.Feed
	logsAcceptedFeed  event.Feed
	txAcceptedFeed    event.Feed
	scope             event.SubscriptionScope

	// This mutex synchronizes chain write operations.
	// Readers don't need to take it, they can just read the database.
	chainmu sync.RWMutex

	receiptsCache *lru.Cache[common.Hash, []*types.Receipt] // Cache for the most recent receipts per block
	txLookupCache *lru.Cache[common.Hash, txLookup]         // Cache for the most recent transaction lookup data.
	badBlocks     *lru.Cache[common.Hash, *badBlock]        // Cache for bad blocks

	stopping atomic.Bool // false if chain is running, true when stopped

	lastAccepted *types.Block // Prevents reorgs past this height

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

	// [wg] is used to wait for the async blockchain processes to finish on shutdown.
	wg sync.WaitGroup

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
}

// NewBlockChain returns a fully initialised block chain using information
// available in the database. It initialises the default Ethereum Validator and
// Processor.
func NewBlockChain(
	db ethdb.Database, cacheConfig *CacheConfig, genesis *Genesis, engine consensus.Engine,
	vmConfig vm.Config, lastAcceptedHash common.Hash, skipChainConfigCheckCompatible bool,
) (*BlockChain, error) {
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
		chainConfig:       chainConfig,
		cacheConfig:       cacheConfig,
		db:                db,
		triedb:            triedb,
		receiptsCache:     lru.NewCache[common.Hash, []*types.Receipt](receiptsCacheLimit),
		txLookupCache:     lru.NewCache[common.Hash, txLookup](txLookupCacheLimit),
		badBlocks:         lru.NewCache[common.Hash, *badBlock](badBlockLimit),
		acceptorQueue:     make(chan *types.Block, cacheConfig.AcceptorQueueLimit),
		acceptedLogsCache: NewFIFOCache[common.Hash, [][]*types.Log](cacheConfig.AcceptedCacheSize),
	}

	headHash := rawdb.ReadHeadBlockHash(db)
	headHeaderHash := rawdb.ReadHeadHeaderHash(db)

	bc.blockChain, err = newBlockChain(db, triedb, cacheConfig.cacheConfig(), genesis, nil, engine, vmConfig, nil, nil)
	if err != nil {
		return nil, err
	}
	// Preserving current behavior for head block
	rawdb.WriteHeadBlockHash(db, headHash)
	rawdb.WriteHeadHeaderHash(db, headHeaderHash)

	bc.hc.InitializeAcceptedNumberCache(cacheConfig.AcceptedCacheSize)

	// Create the state manager
	bc.stateManager = NewTrieWriter(bc.triedb, cacheConfig)

	// Re-generate current block state if it is missing
	if err := bc.loadLastState(lastAcceptedHash); err != nil {
		return nil, err
	}

	// After loading the last state (and reprocessing if necessary), we are
	// guaranteed that [acceptorTip] is equal to [lastAccepted].
	//
	// It is critical to update this vaue before performing any state repairs so
	// that all accepted blocks can be considered.
	bc.acceptorTip = bc.lastAccepted

	// Make sure the state associated with the block is available
	head := bc.CurrentBlock()
	if !bc.HasState(head.Root) {
		return nil, fmt.Errorf("head state missing %d:%s", head.Number, head.Hash())
	}

	if err := bc.protectTrieIndex(); err != nil {
		return nil, err
	}

	// Populate missing tries if required
	if err := bc.populateMissingTries(); err != nil {
		return nil, fmt.Errorf("could not populate missing tries: %v", err)
	}

	// If snapshot initialization is delayed for fast sync, skip initializing it here.
	// This assumes that no blocks will be processed until ResetState is called to initialize
	// the state of fast sync.
	if !bc.cacheConfig.SnapshotDelayInit {
		// Load any existing snapshot, regenerating it if loading failed (if not
		// already initialized in recovery)
		bc.initSnapshot(head)
	}

	// Warm up [hc.acceptedNumberCache] and [acceptedLogsCache]
	bc.warmAcceptedCaches()

	// if txlookup limit is 0 (uindexing disabled), we don't need to repair the tx index tail.
	if bc.cacheConfig.TransactionHistory != 0 {
		latestStateSynced := rawdb.GetLatestSyncPerformed(bc.db)
		bc.repairTxIndexTail(latestStateSynced)
	}

	// Start processing accepted blocks effects in the background
	go bc.startAcceptor()

	// Start tx indexer if it's enabled.
	if bc.cacheConfig.TransactionHistory != 0 {
		bc.txIndexer = newTxIndexer(bc.cacheConfig.TransactionHistory, bc)
	}
	return bc, nil
}

// writeBlockAcceptedIndices writes any indices that must be persisted for accepted block.
// This includes the following:
// - transaction lookup indices
// - updating the acceptor tip index
func (bc *BlockChain) writeBlockAcceptedIndices(b *types.Block) error {
	batch := bc.db.NewBatch()
	if err := bc.batchBlockAcceptedIndices(batch, b); err != nil {
		return err
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("%w: failed to write accepted indices entries batch", err)
	}
	return nil
}

func (bc *BlockChain) batchBlockAcceptedIndices(batch ethdb.Batch, b *types.Block) error {
	if !bc.cacheConfig.SkipTxIndexing {
		rawdb.WriteTxLookupEntriesByBlock(batch, b)
	}
	if err := rawdb.WriteAcceptorTip(batch, b.Hash()); err != nil {
		return fmt.Errorf("%w: failed to write acceptor tip key", err)
	}
	return nil
}

// flattenSnapshot attempts to flatten a block of [hash] to disk.
func (bc *BlockChain) flattenSnapshot(postAbortWork func() error, hash common.Hash) error {
	// If snapshots are not initialized, perform [postAbortWork] immediately.
	if bc.snaps == nil {
		return postAbortWork()
	}

	// Abort snapshot generation before pruning anything from trie database
	// (could occur in AcceptTrie)
	bc.snaps.AbortGeneration()

	// Perform work after snapshot generation is aborted (typically trie updates)
	if err := postAbortWork(); err != nil {
		return err
	}

	// Ensure we avoid flattening the snapshot while we are processing a block, or
	// block execution will fallback to reading from the trie (which is much
	// slower).
	bc.flattenLock.Lock()
	defer bc.flattenLock.Unlock()

	// Flatten the entire snap Trie to disk
	//
	// Note: This resumes snapshot generation.
	return bc.snaps.Flatten(hash)
}

// warmAcceptedCaches fetches previously accepted headers and logs from disk to
// pre-populate [hc.acceptedNumberCache] and [acceptedLogsCache].
func (bc *BlockChain) warmAcceptedCaches() {
	var (
		startTime       = time.Now()
		lastAccepted    = bc.LastAcceptedBlock().NumberU64()
		startIndex      = uint64(1)
		targetCacheSize = uint64(bc.cacheConfig.AcceptedCacheSize)
	)
	if targetCacheSize == 0 {
		log.Info("Not warming accepted cache because disabled")
		return
	}
	if lastAccepted < startIndex {
		// This could occur if we haven't accepted any blocks yet
		log.Info("Not warming accepted cache because there are no accepted blocks")
		return
	}
	cacheDiff := targetCacheSize - 1 // last accepted lookback is inclusive, so we reduce size by 1
	if cacheDiff < lastAccepted {
		startIndex = lastAccepted - cacheDiff
	}
	for i := startIndex; i <= lastAccepted; i++ {
		block := bc.GetBlockByNumber(i)
		if block == nil {
			// This could happen if a node state-synced
			log.Info("Exiting accepted cache warming early because header is nil", "height", i, "t", time.Since(startTime))
			break
		}
		// TODO: handle blocks written to disk during state sync
		bc.hc.acceptedNumberCache.Put(block.NumberU64(), block.Header())
		logs := bc.collectUnflattenedLogs(block, false)
		bc.acceptedLogsCache.Put(block.Hash(), logs)
	}
	log.Info("Warmed accepted caches", "start", startIndex, "end", lastAccepted, "t", time.Since(startTime))
}

// startAcceptor starts processing items on the [acceptorQueue]. If a [nil]
// object is placed on the [acceptorQueue], the [startAcceptor] will exit.
func (bc *BlockChain) startAcceptor() {
	log.Info("Starting Acceptor", "queue length", bc.cacheConfig.AcceptorQueueLimit)

	for next := range bc.acceptorQueue {
		start := time.Now()
		acceptorQueueGauge.Dec(1)

		if err := bc.flattenSnapshot(func() error {
			return bc.stateManager.AcceptTrie(next)
		}, next.Hash()); err != nil {
			log.Crit("unable to flatten snapshot from acceptor", "blockHash", next.Hash(), "err", err)
		}

		// Update last processed and transaction lookup index
		if err := bc.writeBlockAcceptedIndices(next); err != nil {
			log.Crit("failed to write accepted block effects", "err", err)
		}

		// Ensure [hc.acceptedNumberCache] and [acceptedLogsCache] have latest content
		bc.hc.acceptedNumberCache.Put(next.NumberU64(), next.Header())
		logs := bc.collectUnflattenedLogs(next, false)
		bc.acceptedLogsCache.Put(next.Hash(), logs)

		// Update the acceptor tip before sending events to ensure that any client acting based off of
		// the events observes the updated acceptorTip on subsequent requests
		bc.acceptorTipLock.Lock()
		bc.acceptorTip = next
		bc.acceptorTipLock.Unlock()

		// Update accepted feeds
		flattenedLogs := types.FlattenLogs(logs)
		bc.chainAcceptedFeed.Send(ChainEvent{Block: next, Hash: next.Hash(), Logs: flattenedLogs})
		if len(flattenedLogs) > 0 {
			bc.logsAcceptedFeed.Send(flattenedLogs)
		}
		if len(next.Transactions()) != 0 {
			bc.txAcceptedFeed.Send(NewTxsEvent{next.Transactions()})
		}

		bc.acceptorWg.Done()

		acceptorWorkTimer.Inc(time.Since(start).Milliseconds())
		acceptorWorkCount.Inc(1)
		// Note: in contrast to most accepted metrics, we increment the accepted log metrics in the acceptor queue because
		// the logs are already processed in the acceptor queue.
		acceptedLogsCounter.Inc(int64(len(logs)))
	}
}

// addAcceptorQueue adds a new *types.Block to the [acceptorQueue]. This will
// block if there are [AcceptorQueueLimit] items in [acceptorQueue].
func (bc *BlockChain) addAcceptorQueue(b *types.Block) {
	// We only acquire a read lock here because it is ok to add items to the
	// [acceptorQueue] concurrently.
	bc.acceptorClosingLock.RLock()
	defer bc.acceptorClosingLock.RUnlock()

	if bc.acceptorClosed {
		return
	}

	acceptorQueueGauge.Inc(1)
	bc.acceptorWg.Add(1)
	bc.acceptorQueue <- b
}

// DrainAcceptorQueue blocks until all items in [acceptorQueue] have been
// processed.
func (bc *BlockChain) DrainAcceptorQueue() {
	bc.acceptorClosingLock.RLock()
	defer bc.acceptorClosingLock.RUnlock()

	if bc.acceptorClosed {
		return
	}

	bc.acceptorWg.Wait()
}

// stopAcceptor sends a signal to the Acceptor to stop processing accepted
// blocks. The Acceptor will exit once all items in [acceptorQueue] have been
// processed.
func (bc *BlockChain) stopAcceptor() {
	bc.acceptorClosingLock.Lock()
	defer bc.acceptorClosingLock.Unlock()

	// If [acceptorClosed] is already false, we should just return here instead
	// of attempting to close [acceptorQueue] more than once (will cause
	// a panic).
	//
	// This typically happens when a test calls [stopAcceptor] directly (prior to
	// shutdown) and then [stopAcceptor] is called again in shutdown.
	if bc.acceptorClosed {
		return
	}

	// Although nothing should be added to [acceptorQueue] after
	// [acceptorClosed] is updated, we close the channel so the Acceptor
	// goroutine exits.
	bc.acceptorWg.Wait()
	bc.acceptorClosed = true
	close(bc.acceptorQueue)
}

func (bc *BlockChain) InitializeSnapshots() {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	head := bc.CurrentBlock()
	bc.initSnapshot(head)
}

// SenderCacher returns the *TxSenderCacher used within the core package.
func (bc *BlockChain) SenderCacher() *TxSenderCacher {
	return bc.senderCacher
}

// loadLastState loads the last known chain state from the database. This method
// assumes that the chain manager mutex is held.
func (bc *BlockChain) loadLastState(lastAcceptedHash common.Hash) error {
	// Initialize genesis state
	if lastAcceptedHash == (common.Hash{}) {
		bc.lastAccepted = bc.Genesis()
		bc.SetFinalized(bc.Genesis().Header())
		return bc.ResetWithGenesisBlock(bc.Genesis())
	}

	// Restore the last known head block
	head := rawdb.ReadHeadBlockHash(bc.db)
	if head == (common.Hash{}) {
		return errors.New("could not read head block hash")
	}
	// Make sure the entire head block is available
	headBlock := bc.GetBlockByHash(head)
	if headBlock == nil {
		return fmt.Errorf("could not load head block %s", head.Hex())
	}
	// Everything seems to be fine, set as the head block
	bc.currentBlock.Store(headBlock.Header())

	// Restore the last known head header
	currentHeader := headBlock.Header()
	if head := rawdb.ReadHeadHeaderHash(bc.db); head != (common.Hash{}) {
		if header := bc.GetHeaderByHash(head); header != nil {
			currentHeader = header
		}
	}
	bc.hc.SetCurrentHeader(currentHeader)

	log.Info("Loaded most recent local header", "number", currentHeader.Number, "hash", currentHeader.Hash(), "age", common.PrettyAge(time.Unix(int64(currentHeader.Time), 0)))
	log.Info("Loaded most recent local full block", "number", headBlock.Number(), "hash", headBlock.Hash(), "age", common.PrettyAge(time.Unix(int64(headBlock.Time()), 0)))

	// Otherwise, set the last accepted block and perform a re-org.
	bc.lastAccepted = bc.GetBlockByHash(lastAcceptedHash)
	if bc.lastAccepted == nil {
		return fmt.Errorf("could not load last accepted block")
	}
	bc.SetFinalized(bc.lastAccepted.Header())

	// This ensures that the head block is updated to the last accepted block on startup
	if err := bc.setPreference(bc.lastAccepted); err != nil {
		return fmt.Errorf("failed to set preference to last accepted block while loading last state: %w", err)
	}

	// reprocessState is necessary to ensure that the last accepted state is
	// available. The state may not be available if it was not committed due
	// to an unclean shutdown.
	return bc.reprocessState(bc.lastAccepted, 2*bc.cacheConfig.CommitInterval)
}

// ValidateCanonicalChain confirms a canonical chain is well-formed.
func (bc *BlockChain) ValidateCanonicalChain() error {
	// Ensure all accepted blocks are fully processed
	bc.DrainAcceptorQueue()

	current := bc.CurrentBlock()
	i := 0
	log.Info("Beginning to validate canonical chain", "startBlock", current.Number)

	for current.Hash() != bc.Genesis().Hash() {
		blkByHash := bc.GetBlockByHash(current.Hash())
		if blkByHash == nil {
			return fmt.Errorf("couldn't find block by hash %s at height %d", current.Hash().String(), current.Number)
		}
		if blkByHash.Hash() != current.Hash() {
			return fmt.Errorf("blockByHash returned a block with an unexpected hash: %s, expected: %s", blkByHash.Hash().String(), current.Hash().String())
		}
		blkByNumber := bc.GetBlockByNumber(current.Number.Uint64())
		if blkByNumber == nil {
			return fmt.Errorf("couldn't find block by number at height %d", current.Number)
		}
		if blkByNumber.Hash() != current.Hash() {
			return fmt.Errorf("blockByNumber returned a block with unexpected hash: %s, expected: %s", blkByNumber.Hash().String(), current.Hash().String())
		}

		hdrByHash := bc.GetHeaderByHash(current.Hash())
		if hdrByHash == nil {
			return fmt.Errorf("couldn't find block header by hash %s at height %d", current.Hash().String(), current.Number)
		}
		if hdrByHash.Hash() != current.Hash() {
			return fmt.Errorf("hdrByHash returned a block header with an unexpected hash: %s, expected: %s", hdrByHash.Hash().String(), current.Hash().String())
		}
		hdrByNumber := bc.GetHeaderByNumber(current.Number.Uint64())
		if hdrByNumber == nil {
			return fmt.Errorf("couldn't find block header by number at height %d", current.Number)
		}
		if hdrByNumber.Hash() != current.Hash() {
			return fmt.Errorf("hdrByNumber returned a block header with unexpected hash: %s, expected: %s", hdrByNumber.Hash().String(), current.Hash().String())
		}

		// Lookup the full block to get the transactions
		block := bc.GetBlock(current.Hash(), current.Number.Uint64())
		if block == nil {
			log.Error("Current block not found in database", "block", current.Number, "hash", current.Hash())
			return fmt.Errorf("current block missing: #%d [%x..]", current.Number, current.Hash().Bytes()[:4])
		}
		txs := block.Transactions()

		// Transactions are only indexed beneath the last accepted block, so we only check
		// that the transactions have been indexed, if we are checking below the last accepted
		// block.
		shouldIndexTxs := !bc.cacheConfig.SkipTxIndexing &&
			(bc.cacheConfig.TransactionHistory == 0 || bc.lastAccepted.NumberU64() < current.Number.Uint64()+bc.cacheConfig.TransactionHistory)
		if current.Number.Uint64() <= bc.lastAccepted.NumberU64() && shouldIndexTxs {
			// Ensure that all of the transactions have been stored correctly in the canonical
			// chain
			for txIndex, tx := range txs {
				txLookup, _, _ := bc.GetTransactionLookup(tx.Hash())
				if txLookup == nil {
					return fmt.Errorf("failed to find transaction %s", tx.Hash().String())
				}
				if txLookup.BlockHash != current.Hash() {
					return fmt.Errorf("tx lookup returned with incorrect block hash: %s, expected: %s", txLookup.BlockHash.String(), current.Hash().String())
				}
				if txLookup.BlockIndex != current.Number.Uint64() {
					return fmt.Errorf("tx lookup returned with incorrect block index: %d, expected: %d", txLookup.BlockIndex, current.Number)
				}
				if txLookup.Index != uint64(txIndex) {
					return fmt.Errorf("tx lookup returned with incorrect transaction index: %d, expected: %d", txLookup.Index, txIndex)
				}
			}
		}

		blkReceipts := bc.GetReceiptsByHash(current.Hash())
		if blkReceipts.Len() != len(txs) {
			return fmt.Errorf("found %d transaction receipts, expected %d", blkReceipts.Len(), len(txs))
		}
		for index, txReceipt := range blkReceipts {
			if txReceipt.TxHash != txs[index].Hash() {
				return fmt.Errorf("transaction receipt mismatch, expected %s, but found: %s", txs[index].Hash().String(), txReceipt.TxHash.String())
			}
			if txReceipt.BlockHash != current.Hash() {
				return fmt.Errorf("transaction receipt had block hash %s, but expected %s", txReceipt.BlockHash.String(), current.Hash().String())
			}
			if txReceipt.BlockNumber.Uint64() != current.Number.Uint64() {
				return fmt.Errorf("transaction receipt had block number %d, but expected %d", txReceipt.BlockNumber.Uint64(), current.Number)
			}
		}

		i += 1
		if i%1000 == 0 {
			log.Info("Validate Canonical Chain Update", "totalBlocks", i)
		}

		parent := bc.GetHeaderByHash(current.ParentHash)
		if parent.Hash() != current.ParentHash {
			return fmt.Errorf("getBlockByHash retrieved parent block with incorrect hash, found %s, expected: %s", parent.Hash().String(), current.ParentHash.String())
		}
		current = parent
	}

	return nil
}

// stopWithoutSaving stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt. This method stops all running
// goroutines, but does not do all the post-stop work of persisting data.
// OBS! It is generally recommended to use the Stop method!
// This method has been exposed to allow tests to stop the blockchain while simulating
// a crash.
func (bc *BlockChain) stopWithoutSaving(stopInner bool) {
	if !bc.stopping.CompareAndSwap(false, true) {
		return
	}
	// Signal shutdown tx indexer.
	if bc.txIndexer != nil {
		bc.txIndexer.close()
	}

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

	if stopInner {
		bc.blockChain.stopWithoutSaving()
	}
}

// Stop stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt.
func (bc *BlockChain) Stop() {
	bc.stopWithoutSaving(false)

	if _, err := bc.db.Has([]byte{}); err == nil {
		// This prevents us from stopping the blockchain if the database is already
		// closed.
		bc.blockChain.Stop()
	} else {
		bc.blockChain.stopWithoutSaving()
	}

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
	log.Info("Shutting down state manager")
	start := time.Now()
	if err := bc.stateManager.Shutdown(); err != nil {
		log.Error("Failed to Shutdown state manager", "err", err)
	}
	log.Info("State manager shut down", "t", time.Since(start))
	// Close the trie database, release all the held resources as the last step.
	if err := bc.triedb.Close(); err != nil {
		log.Error("Failed to close trie database", "err", err)
	}
	log.Info("Blockchain stopped")
}

// SetPreference attempts to update the head block to be the provided block and
// emits a ChainHeadEvent if successful. This function will handle all reorg
// side effects, if necessary.
//
// Note: This function should ONLY be called on blocks that have already been
// inserted into the chain.
//
// Assumes [bc.chainmu] is not held by the caller.
func (bc *BlockChain) SetPreference(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	return bc.setPreference(block)
}

// setPreference attempts to update the head block to be the provided block and
// emits a ChainHeadEvent if successful. This function will handle all reorg
// side effects, if necessary.
//
// Assumes [bc.chainmu] is held by the caller.
func (bc *BlockChain) setPreference(block *types.Block) error {
	current := bc.CurrentBlock()

	// Return early if the current block is already the block
	// we are trying to write.
	if current.Hash() == block.Hash() {
		return nil
	}

	log.Debug("Setting preference", "number", block.Number(), "hash", block.Hash())
	_, err := bc.blockChain.SetCanonical(block)
	return err
}

// LastConsensusAcceptedBlock returns the last block to be marked as accepted. It may or
// may not yet be processed.
func (bc *BlockChain) LastConsensusAcceptedBlock() *types.Block {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	return bc.lastAccepted
}

// LastAcceptedBlock returns the last block to be marked as accepted and is
// processed.
//
// Note: During initialization, [acceptorTip] is equal to [lastAccepted].
func (bc *BlockChain) LastAcceptedBlock() *types.Block {
	bc.acceptorTipLock.Lock()
	defer bc.acceptorTipLock.Unlock()

	return bc.acceptorTip
}

// Accept sets a minimum height at which no reorg can pass. Additionally,
// this function may trigger a reorg if the block being accepted is not in the
// canonical chain.
//
// Assumes [bc.chainmu] is not held by the caller.
func (bc *BlockChain) Accept(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// The parent of [block] must be the last accepted block.
	if bc.lastAccepted.Hash() != block.ParentHash() {
		return fmt.Errorf(
			"expected accepted block to have parent %s:%d but got %s:%d",
			bc.lastAccepted.Hash().Hex(),
			bc.lastAccepted.NumberU64(),
			block.ParentHash().Hex(),
			block.NumberU64()-1,
		)
	}

	// If the canonical hash at the block height does not match the block we are
	// accepting, we need to trigger a reorg.
	canonical := bc.GetCanonicalHash(block.NumberU64())
	if canonical != block.Hash() {
		log.Debug("Accepting block in non-canonical chain", "number", block.Number(), "hash", block.Hash())
		if err := bc.setPreference(block); err != nil {
			return fmt.Errorf("could not set new preferred block %d:%s as preferred: %w", block.Number(), block.Hash(), err)
		}
	}

	// Enqueue block in the acceptor
	bc.lastAccepted = block
	bc.SetFinalized(block.Header())
	bc.addAcceptorQueue(block)
	acceptedBlockGasUsedCounter.Inc(int64(block.GasUsed()))
	acceptedTxsCounter.Inc(int64(len(block.Transactions())))
	return nil
}

func (bc *BlockChain) Reject(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// Reject Trie
	if err := bc.stateManager.RejectTrie(block); err != nil {
		return fmt.Errorf("unable to reject trie: %w", err)
	}

	if bc.snaps != nil {
		if err := bc.snaps.Discard(block.Hash()); err != nil {
			log.Error("unable to discard snap from rejected block", "block", block.Hash(), "number", block.NumberU64(), "root", block.Root())
		}
	}

	// Remove the block since its data is no longer needed
	batch := bc.db.NewBatch()
	rawdb.DeleteBlock(batch, block.Hash(), block.NumberU64())
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write delete block batch: %w", err)
	}

	// Remove the block from the block cache (ignore return value of whether it was in the cache)
	_ = bc.blockCache.Remove(block.Hash())

	return nil
}

// InsertChain attempts to insert the given batch of blocks in to the canonical
// chain or, otherwise, create a fork. If an error is returned it will return
// the index number of the failing block as well an error describing what went
// wrong.
//
// After insertion is done, all accumulated events will be fired.
func (bc *BlockChain) InsertChain(chain types.Blocks) (int, error) {
	// Sanity check that we have something meaningful to import
	if len(chain) == 0 {
		return 0, nil
	}

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
			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, prev.NumberU64(),
				prev.Hash().Bytes()[:4], i, block.NumberU64(), block.Hash().Bytes()[:4], block.ParentHash().Bytes()[:4])
		}
	}
	// Pre-checks passed, start the full block imports
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()
	for n, block := range chain {
		if err := bc.insertBlock(block, true); err != nil {
			return n, err
		}
	}

	return len(chain), nil
}

func (bc *BlockChain) InsertBlock(block *types.Block) error {
	return bc.InsertBlockManual(block, true)
}

func (bc *BlockChain) InsertBlockManual(block *types.Block, writes bool) error {
	bc.chainmu.Lock()
	err := bc.insertBlock(block, writes)
	bc.chainmu.Unlock()

	return err
}

func (bc *BlockChain) insertBlock(block *types.Block, writes bool) error {
	var opts []InsertOption
	if !writes {
		opts = append(opts, NoWrites)
	}
	// If snapshots are enabled, WithBlockHashes must be called as snapshot layers
	// are stored by block hash.
	if bc.snaps != nil {
		bc.snaps.WithBlockHashes(block.Hash(), block.ParentHash())
	}
	_, err := bc.blockChain.insertChain([]*types.Block{block}, true, opts...)
	if err != nil {
		return err
	}
	parent := bc.GetHeaderByHash(block.ParentHash())
	// Upstream does not perform a snapshot update if the root is the same as the
	// parent root, however here the snapshots are based on the block hash, so
	// this update is necessary.
	if bc.snaps != nil && block.Root() == parent.Root {
		if err := bc.snaps.Update(block.Root(), parent.Root, nil, nil, nil); err != nil {
			return err
		}
	}

	// If node is running in path mode, skip explicit gc operation
	// which is unnecessary in this mode.
	if !writes || bc.triedb.Scheme() == rawdb.PathScheme {
		return nil
	}

	// Note: if InsertTrie must be the last step in verification that can return an error.
	// This allows [stateManager] to assume that if it inserts a trie without returning an
	// error then the block has passed verification and either AcceptTrie/RejectTrie will
	// eventually be called on [root] unless a fatal error occurs. It does not assume that
	// the node will not shutdown before either AcceptTrie/RejectTrie is called.
	if err := bc.stateManager.InsertTrie(block); err != nil {
		if bc.snaps != nil {
			discardErr := bc.snaps.Discard(block.Hash())
			if discardErr != nil {
				log.Debug("failed to discard snapshot after being unable to insert block trie", "block", block.Hash(), "root", block.Root())
			}
		}
		return err
	}
	return nil
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
		receiptLogs := make([]*types.Log, len(receipt.Logs))
		for i, log := range receipt.Logs {
			if removed {
				log.Removed = true
			}
			receiptLogs[i] = log
		}
		logs[i] = receiptLogs
	}
	return logs
}

type badBlock struct {
	block  *types.Block
	reason *BadBlockReason
}

type BadBlockReason struct {
	ChainConfig *params.ChainConfig `json:"chainConfig"`
	Receipts    types.Receipts      `json:"receipts"`
	Number      uint64              `json:"number"`
	Hash        common.Hash         `json:"hash"`
	Error       string              `json:"error"`
}

func (b *BadBlockReason) String() string {
	var receiptString string
	for i, receipt := range b.Receipts {
		receiptString += fmt.Sprintf("\n  %d: cumulative: %v gas: %v contract: %v status: %v tx: %v logs: %v bloom: %x state: %x",
			i, receipt.CumulativeGasUsed, receipt.GasUsed, receipt.ContractAddress.Hex(),
			receipt.Status, receipt.TxHash.Hex(), receipt.Logs, receipt.Bloom, receipt.PostState)
	}
	version, vcs := version.Info()
	platform := fmt.Sprintf("%s %s %s %s", version, runtime.Version(), runtime.GOARCH, runtime.GOOS)
	if vcs != "" {
		vcs = fmt.Sprintf("\nVCS: %s", vcs)
	}
	return fmt.Sprintf(`
########## BAD BLOCK #########
Block: %v (%#x)
Error: %v
Platform: %v%v
Chain config: %#v
Receipts: %v
##############################
`, b.Number, b.Hash, b.Error, platform, vcs, b.ChainConfig, receiptString)
}

// BadBlocks returns a list of the last 'bad blocks' that the client has seen on the network and the BadBlockReason
// that caused each to be reported as a bad block.
// BadBlocks ensures that the length of the blocks and the BadBlockReason slice have the same length.
func (bc *BlockChain) BadBlocks() ([]*types.Block, []*BadBlockReason) {
	blocks := make([]*types.Block, 0, bc.badBlocks.Len())
	reasons := make([]*BadBlockReason, 0, bc.badBlocks.Len())
	for _, hash := range bc.badBlocks.Keys() {
		if badBlk, exist := bc.badBlocks.Peek(hash); exist {
			blocks = append(blocks, badBlk.block)
			reasons = append(reasons, badBlk.reason)
		}
	}
	return blocks, reasons
}

// addBadBlock adds a bad block to the bad-block LRU cache
func (bc *BlockChain) addBadBlock(block *types.Block, reason *BadBlockReason) {
	bc.badBlocks.Add(block.Hash(), &badBlock{
		block:  block,
		reason: reason,
	})
}

// reportBlock logs a bad block error.
func (bc *BlockChain) reportBlock(block *types.Block, receipts types.Receipts, err error) {
	reason := &BadBlockReason{
		ChainConfig: bc.chainConfig,
		Receipts:    receipts,
		Number:      block.NumberU64(),
		Hash:        block.Hash(),
		Error:       err.Error(),
	}

	badBlockCounter.Inc(1)
	bc.addBadBlock(block, reason)
	log.Debug(reason.String())
}

// reprocessBlock reprocesses a previously accepted block. This is often used
// to regenerate previously pruned state tries.
func (bc *BlockChain) reprocessBlock(parent *types.Block, current *types.Block) (common.Hash, error) {
	// If snapshots are enabled, WithBlockHashes must be called as snapshot layers
	// are stored by block hash.
	if bc.snaps != nil {
		bc.snaps.WithBlockHashes(current.Hash(), current.ParentHash())
	}
	if _, err := bc.blockChain.insertChain([]*types.Block{current}, true); err != nil {
		return common.Hash{}, err
	}
	log.Debug("Processed block", "block", current.Hash(), "number", current.NumberU64())

	root := current.Root()
	// Upstream does not perform a snapshot update if the root is the same as the
	// parent root, however here the snapshots are based on the block hash, so
	// this update is necessary.
	if bc.snaps != nil && root == parent.Root() {
		if err := bc.snaps.Update(root, parent.Root(), nil, nil, nil); err != nil {
			return common.Hash{}, err
		}
	}
	return root, nil
}

// initSnapshot instantiates a Snapshot instance and adds it to [bc]
func (bc *blockChain) initSnapshot(b *types.Header) {
	if bc.cacheConfig.SnapshotLimit <= 0 || bc.snaps != nil {
		return
	}

	// If we are starting from genesis, generate the original snapshot disk layer
	// up front, so we can use it while executing blocks in bootstrapping. This
	// also avoids a costly async generation process when reaching tip.
	//
	// Additionally, we should always repair a snapshot if starting at genesis
	// if [SnapshotLimit] > 0.
	asyncBuild := !bc.cacheConfig.SnapshotWait && b.Number.Uint64() > 0
	noBuild := bc.cacheConfig.SnapshotNoBuild && b.Number.Uint64() > 0
	log.Info("Initializing snapshots", "async", asyncBuild, "rebuild", !noBuild, "headHash", b.Hash(), "headRoot", b.Root)
	snapconfig := snapshot.Config{
		CacheSize:  bc.cacheConfig.SnapshotLimit,
		NoBuild:    noBuild,
		AsyncBuild: asyncBuild,
		SkipVerify: !bc.cacheConfig.SnapshotVerify,
	}
	var err error
	bc.snaps, err = snapshot.New(snapconfig, bc.db, bc.triedb, b.Hash(), b.Root)
	if err != nil {
		log.Error("failed to initialize snapshots", "headHash", b.Hash(), "headRoot", b.Root, "err", err, "async", asyncBuild)
	}
}

// reprocessState reprocesses the state up to [block], iterating through its ancestors until
// it reaches a block with a state committed to the database. reprocessState does not use
// snapshots since the disk layer for snapshots will most likely be above the last committed
// state that reprocessing will start from.
func (bc *BlockChain) reprocessState(current *types.Block, reexec uint64) error {
	origin := current.NumberU64()
	acceptorTip, err := rawdb.ReadAcceptorTip(bc.db)
	if err != nil {
		return fmt.Errorf("%w: unable to get Acceptor tip", err)
	}
	log.Info("Loaded Acceptor tip", "hash", acceptorTip)

	// The acceptor tip is up to date either if it matches the current hash, or it has not been
	// initialized (i.e., this node has not accepted any blocks asynchronously).
	acceptorTipUpToDate := acceptorTip == (common.Hash{}) || acceptorTip == current.Hash()

	// If the state is already available and the acceptor tip is up to date, skip re-processing.
	if bc.HasState(current.Root()) && acceptorTipUpToDate {
		log.Info("Skipping state reprocessing", "root", current.Root())
		return nil
	}

	// If the acceptorTip is a non-empty hash, jump re-processing back to the acceptor tip to ensure that
	// we re-process at a minimum from the last processed accepted block.
	// Note: we do not have a guarantee that the last trie on disk will be at a height <= acceptorTip.
	// Since we need to re-process from at least the acceptorTip to ensure indices are updated correctly
	// we must start searching for the block to start re-processing at the acceptorTip.
	// This may occur if we are running in archive mode where every block's trie is committed on insertion
	// or during an unclean shutdown.
	if acceptorTip != (common.Hash{}) {
		current = bc.GetBlockByHash(acceptorTip)
		if current == nil {
			return fmt.Errorf("failed to get block for acceptor tip %s", acceptorTip)
		}
	}

	for i := 0; i < int(reexec); i++ {
		// TODO: handle canceled context

		if current.NumberU64() == 0 {
			return errors.New("genesis state is missing")
		}
		parent := bc.GetBlock(current.ParentHash(), current.NumberU64()-1)
		if parent == nil {
			return fmt.Errorf("missing block %s:%d", current.ParentHash().Hex(), current.NumberU64()-1)
		}
		current = parent
		_, err = bc.stateCache.OpenTrie(current.Root())
		if err == nil {
			break
		}
	}
	if err != nil {
		switch err.(type) {
		case *trie.MissingNodeError:
			return fmt.Errorf("required historical state unavailable (reexec=%d)", reexec)
		default:
			return err
		}
	}

	// State was available at historical point, regenerate
	var (
		start        = time.Now()
		logged       time.Time
		previousRoot common.Hash
		triedb       = bc.triedb
		writeIndices bool
	)
	// Note: we add 1 since in each iteration, we attempt to re-execute the next block.
	log.Info("Re-executing blocks to generate state for last accepted block", "from", current.NumberU64()+1, "to", origin)
	for current.NumberU64() < origin {
		// TODO: handle canceled context

		// Print progress logs if long enough time elapsed
		if time.Since(logged) > 8*time.Second {
			log.Info("Regenerating historical state", "block", current.NumberU64()+1, "target", origin, "remaining", origin-current.NumberU64(), "elapsed", time.Since(start))
			logged = time.Now()
		}

		// Retrieve the next block to regenerate and process it
		parent := current
		next := current.NumberU64() + 1
		if current = bc.GetBlockByNumber(next); current == nil {
			return fmt.Errorf("failed to retrieve block %d while re-generating state", next)
		}

		// Initialize snapshot if required (prevents full snapshot re-generation in
		// the case of unclean shutdown)
		if parent.Hash() == acceptorTip {
			log.Info("Recovering snapshot", "hash", parent.Hash(), "index", parent.NumberU64())
			// TODO: switch to checking the snapshot block hash markers here to ensure that when we re-process the block, we have the opportunity to apply
			// a snapshot diff layer that we may have been in the middle of committing during shutdown. This will prevent snapshot re-generation in the case
			// that the node stops mid-way through snapshot flattening (performed across multiple DB batches).
			// If snapshot initialization is delayed due to state sync, skip initializing snaps here
			if !bc.cacheConfig.SnapshotDelayInit {
				bc.initSnapshot(parent.Header())
			}
			writeIndices = true // Set [writeIndices] to true, so that the indices will be updated from the last accepted tip onwards.
		}

		// Reprocess next block using previously fetched data
		root, err := bc.reprocessBlock(parent, current)
		if err != nil {
			return err
		}

		// Flatten snapshot if initialized, holding a reference to the state root until the next block
		// is processed.
		if err := bc.flattenSnapshot(func() error {
			if previousRoot != (common.Hash{}) {
				triedb.Dereference(previousRoot)
			}
			previousRoot = root
			return nil
		}, current.Hash()); err != nil {
			return err
		}

		// Write any unsaved indices to disk
		if writeIndices {
			if err := bc.writeBlockAcceptedIndices(current); err != nil {
				return fmt.Errorf("%w: failed to process accepted block indices", err)
			}
		}
	}

	_, nodes, imgs := triedb.Size()
	log.Info("Historical state regenerated", "block", current.NumberU64(), "elapsed", time.Since(start), "nodes", nodes, "preimages", imgs)
	if previousRoot != (common.Hash{}) {
		return triedb.Commit(previousRoot, true)
	}
	return nil
}

func (bc *BlockChain) protectTrieIndex() error {
	if !bc.cacheConfig.Pruning {
		return rawdb.WritePruningDisabled(bc.db)
	}
	pruningDisabled, err := rawdb.HasPruningDisabled(bc.db)
	if err != nil {
		return fmt.Errorf("failed to check if the chain has been run with pruning disabled: %w", err)
	}
	if !pruningDisabled {
		return nil
	}
	if !bc.cacheConfig.AllowMissingTries {
		return ErrRefuseToCorruptArchiver
	}
	return nil
}

// populateMissingTries iterates from [bc.cacheConfig.PopulateMissingTries] (defaults to 0)
// to [LastAcceptedBlock] and persists all tries to disk that are not already on disk. This is
// used to fill trie index gaps in an "archive" node without resyncing from scratch.
//
// NOTE: Assumes the genesis root and last accepted root are written to disk
func (bc *BlockChain) populateMissingTries() error {
	if bc.cacheConfig.PopulateMissingTries == nil {
		return nil
	}

	var (
		lastAccepted = bc.LastAcceptedBlock().NumberU64()
		startHeight  = *bc.cacheConfig.PopulateMissingTries
		startTime    = time.Now()
		logged       time.Time
		triedb       = bc.triedb
		missing      = 0
	)

	// Do not allow the config to specify a starting point above the last accepted block.
	if startHeight > lastAccepted {
		return fmt.Errorf("cannot populate missing tries from a starting point (%d) > last accepted block (%d)", startHeight, lastAccepted)
	}

	// If we are starting from the genesis, increment the start height by 1 so we don't attempt to re-process
	// the genesis block.
	if startHeight == 0 {
		startHeight += 1
	}
	parent := bc.GetBlockByNumber(startHeight - 1)
	if parent == nil {
		return fmt.Errorf("failed to fetch initial parent block for re-populate missing tries at height %d", startHeight-1)
	}

	it := newBlockChainIterator(bc, startHeight, bc.cacheConfig.PopulateMissingTriesParallelism)
	defer it.Stop()

	for i := startHeight; i < lastAccepted; i++ {
		// Print progress logs if long enough time elapsed
		if time.Since(logged) > 8*time.Second {
			log.Info("Populating missing tries", "missing", missing, "block", i, "remaining", lastAccepted-i, "elapsed", time.Since(startTime))
			logged = time.Now()
		}

		// TODO: handle canceled context
		current, hasState, err := it.Next(context.TODO())
		if err != nil {
			return fmt.Errorf("error while populating missing tries: %w", err)
		}

		if hasState {
			parent = current
			continue
		}

		root, err := bc.reprocessBlock(parent, current)
		if err != nil {
			return err
		}

		// Commit root to disk so that it can be accessed directly
		if err := triedb.Commit(root, false); err != nil {
			return err
		}
		parent = current
		log.Debug("Populated missing trie", "block", current.NumberU64(), "root", root)
		missing++
	}

	// Write marker to DB to indicate populate missing tries finished successfully.
	// Note: writing the marker here means that we do allow consecutive runs of re-populating
	// missing tries if it does not finish during the prior run.
	if err := rawdb.WritePopulateMissingTries(bc.db); err != nil {
		return fmt.Errorf("failed to write offline pruning success marker: %w", err)
	}

	_, nodes, imgs := triedb.Size()
	log.Info("All missing tries populated", "startHeight", startHeight, "lastAcceptedHeight", lastAccepted, "missing", missing, "elapsed", time.Since(startTime), "nodes", nodes, "preimages", imgs)
	return nil
}

// CleanBlockRootsAboveLastAccepted gathers the blocks that may have previously been in processing above the
// last accepted block and wipes their block roots from disk to mark their tries as inaccessible.
// This is used prior to pruning to ensure that all of the tries that may still be in processing are marked
// as inaccessible and mirrors the handling of middle roots in the geth offline pruning implementation.
// This is not strictly necessary, but maintains a soft assumption.
func (bc *BlockChain) CleanBlockRootsAboveLastAccepted() error {
	targetRoot := bc.LastAcceptedBlock().Root()

	// Clean up any block roots above the last accepted block before we start pruning.
	// Note: this takes the place of middleRoots in the geth implementation since we do not
	// track processing block roots via snapshot journals in the same way.
	processingRoots := bc.gatherBlockRootsAboveLastAccepted()
	// If there is a block above the last accepted block with an identical state root, we
	// explicitly remove it from the set to ensure we do not corrupt the last accepted trie.
	delete(processingRoots, targetRoot)
	for processingRoot := range processingRoots {
		// Delete the processing root from disk to mark the trie as inaccessible (no need to handle this in a batch).
		if err := bc.db.Delete(processingRoot[:]); err != nil {
			return fmt.Errorf("failed to remove processing root (%s) preparing for offline pruning: %w", processingRoot, err)
		}
	}

	return nil
}

// gatherBlockRootsAboveLastAccepted iterates forward from the last accepted block and returns a list of all block roots
// for any blocks that were inserted above the last accepted block.
// Given that we never insert a block into the chain unless all of its ancestors have been inserted, this should gather
// all of the block roots for blocks inserted above the last accepted block that may have been in processing at some point
// in the past and are therefore potentially still acceptable.
// Note: there is an edge case where the node dies while the consensus engine is rejecting a branch of blocks since the
// consensus engine will reject the lowest ancestor first. In this case, these blocks will not be considered acceptable in
// the future.
// Ex.
//
//	   A
//	 /   \
//	B     C
//	|
//	D
//	|
//	E
//	|
//	F
//
// The consensus engine accepts block C and proceeds to reject the other branch in order (B, D, E, F).
// If the consensus engine dies after rejecting block D, block D will be deleted, such that the forward iteration
// may not find any blocks at this height and will not reach the previously processing blocks E and F.
func (bc *BlockChain) gatherBlockRootsAboveLastAccepted() map[common.Hash]struct{} {
	blockRoots := make(map[common.Hash]struct{})
	for height := bc.lastAccepted.NumberU64() + 1; ; height++ {
		blockHashes := rawdb.ReadAllHashes(bc.db, height)
		// If there are no block hashes at [height], then there should be no further acceptable blocks
		// past this point.
		if len(blockHashes) == 0 {
			break
		}

		// Fetch the blocks and append their roots.
		for _, blockHash := range blockHashes {
			block := bc.GetBlockByHash(blockHash)
			if block == nil {
				continue
			}

			blockRoots[block.Root()] = struct{}{}
		}
	}

	return blockRoots
}

// ResetToStateSyncedBlock reinitializes the state of the blockchain
// to the trie represented by [block.Root()] after updating
// in-memory and on disk current block pointers to [block].
// Only should be called after state sync has completed.
func (bc *BlockChain) ResetToStateSyncedBlock(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// Update head block and snapshot pointers on disk
	batch := bc.db.NewBatch()
	if err := bc.batchBlockAcceptedIndices(batch, block); err != nil {
		return err
	}
	rawdb.WriteTd(batch, block.Hash(), block.NumberU64(), block.Difficulty())
	rawdb.WriteHeadBlockHash(batch, block.Hash())
	rawdb.WriteHeadHeaderHash(batch, block.Hash())
	rawdb.WriteSnapshotBlockHash(batch, block.Hash())
	rawdb.WriteSnapshotRoot(batch, block.Root())
	if err := rawdb.WriteSyncPerformed(batch, block.NumberU64()); err != nil {
		return err
	}

	if err := batch.Write(); err != nil {
		return err
	}

	// if txlookup limit is 0 (uindexing disabled), we don't need to repair the tx index tail.
	if bc.cacheConfig.TransactionHistory != 0 {
		bc.repairTxIndexTail(block.NumberU64())
	}

	// Update all in-memory chain markers
	bc.lastAccepted = block
	bc.SetFinalized(block.Header())
	bc.acceptorTip = block
	bc.currentBlock.Store(block.Header())
	bc.hc.SetCurrentHeader(block.Header())

	lastAcceptedHash := block.Hash()
	bc.stateCache = state.NewDatabaseWithNodeDB(bc.db, bc.triedb)

	if err := bc.loadLastState(lastAcceptedHash); err != nil {
		return err
	}
	// Create the state manager
	bc.stateManager = NewTrieWriter(bc.triedb, bc.cacheConfig)

	// Make sure the state associated with the block is available
	head := bc.CurrentBlock()
	if !bc.HasState(head.Root) {
		return fmt.Errorf("head state missing %d:%s", head.Number, head.Hash())
	}

	bc.initSnapshot(head)
	return nil
}

// CacheConfig returns a reference to [bc.cacheConfig]
//
// This is used by [miner] to set prefetch parallelism
// during block building.
func (bc *BlockChain) CacheConfig() *CacheConfig {
	return bc.cacheConfig
}

func (bc *BlockChain) repairTxIndexTail(newTail uint64) error {
	bc.txIndexTailLock.Lock()
	defer bc.txIndexTailLock.Unlock()

	if curr := rawdb.ReadTxIndexTail(bc.db); curr == nil || *curr < newTail {
		log.Info("Repairing tx index tail", "old", curr, "new", newTail)
		rawdb.WriteTxIndexTail(bc.db, newTail)
	}
	return nil
}