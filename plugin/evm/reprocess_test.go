package evm

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

var (
	cChainGenesisFuji    = "{\"config\":{\"chainId\":43113,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	cChainGenesisMainnet = "{\"config\":{\"chainId\":43114,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
)

var (
	sourceDbDir      = "sourceDb"
	sourcePrefix     = ""
	dbDir            = ""
	startBlock       = uint64(0)
	endBlock         = uint64(200)
	prefetchers      = 4
	useSnapshot      = true
	pruning          = true
	skipUpgradeCheck = false

	// merkledb options
	merkleDBBranchFactor          = 16
	valueNodeCacheSizeMB          = 1
	intermediateNodeCacheSizeMB   = 1
	intermediateWriteBufferSizeKB = 1024
	intermediateWriteBatchSizeKB  = 256
)

func TestMain(m *testing.M) {
	flag.StringVar(&sourceDbDir, "sourceDbDir", sourceDbDir, "directory of source database")
	flag.StringVar(&sourcePrefix, "sourcePrefix", sourcePrefix, "prefix of source database")
	flag.StringVar(&dbDir, "dbDir", dbDir, "directory to store database (uses memory if empty)")
	flag.Uint64Var(&startBlock, "startBlock", startBlock, "start block number")
	flag.Uint64Var(&endBlock, "endBlock", endBlock, "end block number")
	flag.IntVar(&prefetchers, "prefetchers", prefetchers, "number of prefetchers")
	flag.BoolVar(&useSnapshot, "useSnapshot", useSnapshot, "use snapshot")
	flag.BoolVar(&pruning, "pruning", pruning, "pruning")
	flag.BoolVar(&skipUpgradeCheck, "skipUpgradeCheck", skipUpgradeCheck, "skip upgrade check")

	// merkledb options
	flag.IntVar(&merkleDBBranchFactor, "merkleDBBranchFactor", merkleDBBranchFactor, "merkleDB branch factor")
	flag.IntVar(&valueNodeCacheSizeMB, "valueNodeCacheSizeMB", valueNodeCacheSizeMB, "value node cache size in MB")
	flag.IntVar(&intermediateNodeCacheSizeMB, "intermediateNodeCacheSizeMB", intermediateNodeCacheSizeMB, "intermediate node cache size in MB")
	flag.IntVar(&intermediateWriteBufferSizeKB, "intermediateWriteBufferSizeKB", intermediateWriteBufferSizeKB, "intermediate write buffer size in KB")
	flag.IntVar(&intermediateWriteBatchSizeKB, "intermediateWriteBatchSizeKB", intermediateWriteBatchSizeKB, "intermediate write batch size in KB")

	flag.Parse()
	m.Run()
}

type prefixReader struct {
	ethdb.Database
	prefix []byte
}

func (r *prefixReader) Get(key []byte) ([]byte, error) {
	return r.Database.Get(append(r.prefix, key...))
}

func (r *prefixReader) Has(key []byte) (bool, error) {
	return r.Database.Has(append(r.prefix, key...))
}

const (
	cacheSize = 128
	handles   = 1024
)

func openSourceDB(t *testing.T) ethdb.Database {
	sourceDb, err := rawdb.NewLevelDBDatabase(sourceDbDir, cacheSize, handles, "", true)
	if err != nil {
		t.Skipf("Failed to open source database: %s", err)
	}
	prefix := []byte(sourcePrefix)
	if bytes.HasPrefix(prefix, []byte("0x")) {
		prefix = prefix[2:]
		var err error
		prefix, err = hex.DecodeString(string(prefix))
		if err != nil {
			t.Fatalf("invalid hex prefix: %s", prefix)
		}
	}
	return &prefixReader{Database: sourceDb, prefix: prefix}
}

func TestExportBlocks(t *testing.T) {
	sourceDb := openSourceDB(t)
	defer sourceDb.Close()

	if startBlock == 0 {
		startBlock = 1
		t.Logf("Start block is 0, setting to 1")
	}

	db, err := rawdb.NewLevelDBDatabase(dbDir, cacheSize, handles, "", false)
	require.NoError(t, err)
	defer db.Close()

	logEach := 100_000
	for i := startBlock; i <= endBlock; i++ {
		hash := rawdb.ReadCanonicalHash(sourceDb, i)
		block := rawdb.ReadBlock(sourceDb, hash, i)
		if block == nil {
			t.Fatalf("Block %d not found", i)
		}
		rawdb.WriteCanonicalHash(db, hash, i)
		rawdb.WriteBlock(db, block)
		if i%uint64(logEach) == 0 {
			t.Logf("Exported block %d", i)
		}
	}

	t.Logf("Exported %d blocks", endBlock-startBlock+1)
}

var (
	VMDBPrefix         = []byte("vm")
	fujiXChainID       = ids.FromStringOrPanic("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
	fujiCChainID       = ids.FromStringOrPanic("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetCChainID    = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

type dbs struct {
	metadata database.Database
	chain    ethdb.Database
	merkledb database.Database

	base database.Database
}

func (d *dbs) Close() { d.base.Close() }

func openDBs(t *testing.T) dbs {
	var base database.Database
	if dbDir == "" {
		base = memdb.New()
	} else {
		db, err := leveldb.New(dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
		require.NoError(t, err)
		base = db
	}

	return dbs{
		metadata: prefixdb.New(reprocessMetadataPrefix, base),
		chain:    rawdb.NewDatabase(Database{prefixdb.New(ethDBPrefix, base)}),
		merkledb: prefixdb.New(merkledbPrefix, base),
		base:     base,
	}
}

var (
	reprocessMetadataPrefix = []byte("metadata")
	merkledbPrefix          = []byte("merkledb")

	lastAcceptedRootKey   = []byte("lastAcceptedRoot")
	lastAcceptedHashKey   = []byte("lastAcceptedHash")
	lastAcceptedHeightKey = []byte("lastAcceptedHeight")
)

func getMetadata(db database.Database) (lastHash, lastRoot common.Hash, lastHeight uint64) {
	if bytes, err := db.Get(lastAcceptedRootKey); err == nil {
		lastRoot = common.BytesToHash(bytes)
	}
	if bytes, err := db.Get(lastAcceptedHashKey); err == nil {
		lastHash = common.BytesToHash(bytes)
	}
	if bytes, err := database.GetUInt64(db, lastAcceptedHeightKey); err == nil {
		lastHeight = bytes
	}

	return lastHash, lastRoot, lastHeight
}

func TestPersistedMetadata(t *testing.T) {
	dbs := openDBs(t)
	defer dbs.Close()

	lastHash, lastRoot, lastHeight := getMetadata(dbs.metadata)
	t.Logf("Last hash: %x, Last root: %x, Last height: %d", lastHash, lastRoot, lastHeight)
}

func TestCalculatePrefix(t *testing.T) {
	prefix := prefixdb.JoinPrefixes(
		prefixdb.MakePrefix(mainnetCChainID[:]),
		VMDBPrefix,
	)

	prefix = append(prefix, prefixdb.MakePrefix(ethDBPrefix)...)
	t.Logf("Prefix: %x", prefix)
}

func init() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGPIPE, syscall.SIGHUP)
	go cleanupOnInterrupt(c)
}

var cf struct {
	o sync.Once
	m sync.RWMutex
	f []func()
}

// cleanupOnInterrupt registers a signal handler and will execute a stack of functions if an interrupt signal is caught
func cleanupOnInterrupt(c chan os.Signal) {
	for range c {
		cf.o.Do(func() {
			cf.m.RLock()
			defer cf.m.RUnlock()
			for i := len(cf.f) - 1; i >= 0; i-- {
				cf.f[i]()
			}
			os.Exit(1)
		})
	}
}

// CleanupOnInterrupt stores cleanup functions to execute if an interrupt signal is caught
func CleanupOnInterrupt(cleanup func()) {
	cf.m.Lock()
	defer cf.m.Unlock()
	cf.f = append(cf.f, cleanup)
}

func TestReprocessGenesis(t *testing.T) {
	for _, backend := range []string{"merkledb", "legacy"} {
		t.Run(backend, func(t *testing.T) { testReprocessGenesis(t, backend) })
	}
}

func TestReprocessMainnetBlocks(t *testing.T) {
	enableLogging()
	source := openSourceDB(t)
	defer source.Close()

	dbs := openDBs(t)
	defer dbs.Close()

	lastHash, lastRoot, lastHeight := getMetadata(dbs.metadata)
	t.Logf("Persisted metadata: Last hash: %x, Last root: %x, Last height: %d", lastHash, lastRoot, lastHeight)

	require.Equal(t, lastHeight, startBlock, "Last height does not match start block")
	if lastHash != (common.Hash{}) {
		// Other than when genesis is not performed, start processing from the next block
		startBlock++
	}

	for _, backend := range []*reprocessBackend{
		getMainnetBackend(t, "merkledb", source, dbs),
		getMainnetBackend(t, "legacy", source, dbs),
	} {
		t.Run(backend.Name, func(t *testing.T) {
			lastHash, lastRoot = reprocess(t, backend, lastHash, lastRoot, startBlock, endBlock)
			t.Logf("Last hash: %x, Last root: %x", lastHash, lastRoot)
		})
	}
}

func testReprocessGenesis(t *testing.T, backendName string) {
	dbs := openDBs(t)
	defer dbs.Close()

	blockCount := endBlock // use the end block as the block count, since we start from 0
	backend := getBackend(t, backendName, int(blockCount), dbs)
	cacheConfig := backend.CacheConfig

	var lastHash, lastRoot common.Hash
	start, stop := uint64(0), blockCount/2
	lastHash, lastRoot = reprocess(t, backend, lastHash, lastRoot, start, stop)
	if cacheConfig.SnapshotLimit > 0 {
		accounts, storages := checkSnapshot(t, backend.Disk, false)
		t.Logf("Iterated snapshot: Accounts: %d, Storages: %d", accounts, storages)
	}

	// Need to re-open backend as the previous one is closed
	backend = getBackend(t, backendName, int(blockCount), dbs)
	start, stop = blockCount/2+1, blockCount
	lastHash, lastRoot = reprocess(t, backend, lastHash, lastRoot, start, stop)
	if cacheConfig.SnapshotLimit > 0 {
		accounts, storages := checkSnapshot(t, backend.Disk, false)
		t.Logf("Iterated snapshot: Accounts: %d, Storages: %d", accounts, storages)
	}
	t.Logf("Last block: %d, Last hash: %x, Last root: %x", stop, lastHash, lastRoot)
}

func reprocess(
	t *testing.T,
	backend *reprocessBackend, lastHash, lastRoot common.Hash,
	start, stop uint64,
) (common.Hash, common.Hash) {
	cacheConfig := backend.CacheConfig
	db := backend.Disk

	var lastInsertedRoot common.Hash
	checkRootFn := func(expected, got common.Hash) bool {
		t.Logf("Got root: %x (original: %x)", got, expected)
		lastInsertedRoot = got
		if backend.VerifyRoot {
			return expected == got
		}
		return true
	}

	var opts []core.Opts
	cacheConfig.SnapshotDelayInit = true
	if start > 0 {
		cacheConfig.SnapshotNoBuild = true                         // after genesis, snapshot must already be available
		opts = append(opts, core.Opts{LastAcceptedRoot: lastRoot}) // after genesis, we must specify the last root
	}
	bc, err := core.NewBlockChain(
		db, &cacheConfig, backend.Genesis, backend.Engine, vm.Config{}, lastHash, skipUpgradeCheck,
		opts...,
	)
	require.NoError(t, err)
	defer bc.Stop()

	var lock sync.Mutex

	CleanupOnInterrupt(func() {
		lock.Lock()
		defer lock.Unlock()

		bc.Stop()
	})

	if start == 0 {
		// Handling the genesis block
		normalGenesis := backend.Genesis.ToBlock()
		require.NoError(t, bc.LoadGenesisState(normalGenesis))

		lastRoot = normalGenesis.Root()
		if backend := cacheConfig.KeyValueDB.KVBackend; backend != nil {
			lastRoot = backend.Root()
		}

		t.Logf("Genesis performed: hash: %x, root : %x", bc.CurrentBlock().Hash(), lastRoot)
		start = 1
	}

	bc.Validator().(*core.BlockValidator).CheckRoot = checkRootFn
	bc.InitializeSnapshots(&core.Opts{LastAcceptedRoot: lastRoot})

	for i := start; i <= stop; i++ {
		block := backend.GetBlock(i)
		isApricotPhase5 := backend.Genesis.Config.IsApricotPhase5(block.Time())
		atomicTxs, err := ExtractAtomicTxs(block.ExtData(), isApricotPhase5, Codec)
		require.NoError(t, err)
		t.Logf("Block: %d, Txs: %d (+ %d atomic), Parent State: %s", i, len(block.Transactions()), len(atomicTxs), lastRoot.TerminalString())

		// Override parentRoot to match last state
		parent := bc.GetHeaderByNumber(block.NumberU64() - 1)
		parent.Root = lastRoot

		// Take lock here to prevent shutdown before block is accepted
		lock.Lock()
		err = bc.InsertBlockManualWithParent(block, parent, true)
		require.NoError(t, err)

		t.Logf("Accepting block %d, was inserted with root: %x, hash: %x", i, lastInsertedRoot, block.Hash())
		errorOnClosed := true // make sure block is accepted
		err = bc.AcceptWithRoot(block, lastInsertedRoot, errorOnClosed)
		require.NoError(t, err)

		lastRoot = lastInsertedRoot
		lastHash = block.Hash()

		// Update metadata
		require.NoError(t, backend.Metadata.Put(lastAcceptedRootKey, lastRoot.Bytes()))
		require.NoError(t, backend.Metadata.Put(lastAcceptedHashKey, lastHash.Bytes()))
		require.NoError(t, database.PutUInt64(backend.Metadata, lastAcceptedHeightKey, i))
		lock.Unlock()
	}

	return lastHash, lastRoot
}

func TestCheckSnapshot(t *testing.T) {
	dbs := openDBs(t)
	defer dbs.Close()

	accounts, storages := checkSnapshot(t, dbs.chain, false)
	t.Logf("Snapshot: Accounts: %d, Storages: %d", accounts, storages)
}

func checkSnapshot(t *testing.T, db ethdb.Database, log bool) (int, int) {
	t.Helper()

	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	defer it.Release()
	accounts := 0
	for it.Next() {
		if len(it.Key()) != 33 {
			continue
		}
		accounts++
		if log {
			t.Logf("Snapshot (account): %x, %x\n", it.Key(), it.Value())
		}
	}

	it2 := db.NewIterator(rawdb.SnapshotStoragePrefix, nil)
	defer it2.Release()
	storages := 0
	for it2.Next() {
		if len(it2.Key()) != 65 {
			continue
		}
		storages++
		if log {
			t.Logf("Snapshot (storage): %x, %x", it2.Key(), it2.Value())
		}
	}
	return accounts, storages
}

func enableLogging() {
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LevelInfo, true)))
}
