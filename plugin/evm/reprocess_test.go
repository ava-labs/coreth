package evm

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/units"
	xmerkledb "github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/shim/merkledb"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

var (
	cChainGenesisFuji    = "{\"config\":{\"chainId\":43113,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	cChainGenesisMainnet = "{\"config\":{\"chainId\":43114,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
)

var (
	sourceDbDir  = "sourceDb"
	sourcePrefix = ""
	dbDir        = "db"
	startBlock   = uint64(0)
	endBlock     = uint64(20_000)
)

func TestMain(m *testing.M) {
	flag.StringVar(&sourceDbDir, "sourceDbDir", sourceDbDir, "directory of source database")
	flag.StringVar(&sourcePrefix, "sourcePrefix", sourcePrefix, "prefix of source database")
	flag.StringVar(&dbDir, "dbDir", dbDir, "directory to store database")
	flag.Uint64Var(&startBlock, "startBlock", startBlock, "start block number")
	flag.Uint64Var(&endBlock, "endBlock", endBlock, "end block number")

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

func TestExportBlocks(t *testing.T) {
	cache := 128
	handles := 1024

	sourceDb, err := rawdb.NewLevelDBDatabase(sourceDbDir, cache, handles, "", true)
	require.NoError(t, err)
	defer sourceDb.Close()

	prefix := []byte(sourcePrefix)
	if bytes.HasPrefix(prefix, []byte("0x")) {
		prefix = prefix[2:]
		var err error
		prefix, err = hex.DecodeString(string(prefix))
		if err != nil {
			t.Fatalf("invalid hex prefix: %s", prefix)
		}
	}
	t.Logf("Using prefix: %x", prefix)
	sourceDb = &prefixReader{Database: sourceDb, prefix: prefix}

	if startBlock == 0 {
		startBlock = 1
		t.Logf("Start block is 0, setting to 1")
	}

	db, err := rawdb.NewLevelDBDatabase(dbDir, cache, handles, "", false)
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
	fujiXChainID    = ids.FromStringOrPanic("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
	fujiCChainID    = ids.FromStringOrPanic("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
	mainnetXChainID = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetCChainID = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	VMDBPrefix      = []byte("vm")
)

func TestCalculatePrefix(t *testing.T) {
	prefix := prefixdb.JoinPrefixes(
		prefixdb.MakePrefix(mainnetCChainID[:]),
		VMDBPrefix,
	)

	prefix = append(prefix, prefixdb.MakePrefix(ethDBPrefix)...)
	t.Logf("Prefix: %x", prefix)
}

func TestReprocessGenesis(t *testing.T) {
	backend := getBackend(t, "test")
	g := backend.Genesis
	engine := backend.Engine

	db := rawdb.NewMemoryDatabase()
	ctx := context.Background()

	mdbKVStore := memdb.New()
	mdb, err := xmerkledb.New(ctx, mdbKVStore, xmerkledb.Config{
		BranchFactor:                xmerkledb.BranchFactor16,
		Hasher:                      xmerkledb.DefaultHasher,
		HistoryLength:               1,
		RootGenConcurrency:          0,
		ValueNodeCacheSize:          units.MiB,
		IntermediateNodeCacheSize:   units.MiB,
		IntermediateWriteBufferSize: units.KiB,
		IntermediateWriteBatchSize:  256 * units.KiB,
		Reg:                         prometheus.NewRegistry(),
		TraceLevel:                  xmerkledb.InfoTrace,
		Tracer:                      trace.Noop,
	})
	require.NoError(t, err)

	cacheConfig := *core.DefaultCacheConfig
	cacheConfig.KeyValueDB = &triedb.KeyValueConfig{
		KVBackend: merkledb.NewMerkleDB(mdb),
	}
	cacheConfig.TriePrefetcherParallelism = 4
	cacheConfig.SnapshotLimit = 256
	cacheConfig.SnapshotDelayInit = true
	// cacheConfig.Pruning = false

	bc, err := core.NewBlockChain(db, &cacheConfig, g, engine, vm.Config{}, common.Hash{}, false)
	require.NoError(t, err)

	var lastInsertedRoot common.Hash
	checkRootFn := func(expected, got common.Hash) bool {
		t.Logf("Got root: %s", got.Hex())
		lastInsertedRoot = got
		return true
	}
	bc.Validator().(*core.BlockValidator).CheckRoot = checkRootFn

	normalGenesis := g.ToBlock()
	require.NoError(t, bc.LoadGenesisState(normalGenesis))

	bc.InitializeSnapshots()

	t.Logf("Genesis block: %s", bc.CurrentBlock().Hash().Hex())
	getCurrentRoot := func() common.Hash {
		return cacheConfig.KeyValueDB.KVBackend.Root()
	}
	lastRoot := getCurrentRoot()
	lastHash := normalGenesis.Hash()

	start, stop := uint64(1), backend.BlockCount/2
	for i := start; i <= stop; i++ {
		block := backend.GetBlock(i)
		t.Logf("Block: %d, Transactions: %d, Parent State: %x", i, len(block.Transactions()), lastRoot)

		// Override parentRoot to match last state
		parent := bc.GetHeaderByNumber(block.NumberU64() - 1)
		originalParentRoot := parent.Root
		parent.Root = lastRoot

		err := bc.InsertBlockManualWithParent(block, parent, true)
		require.NoError(t, err)

		// Restore parent root
		parent.Root = originalParentRoot

		t.Logf("Accepting block %s", block.Hash().Hex())
		err = bc.AcceptWithRoot(block, lastInsertedRoot)
		require.NoError(t, err)

		lastRoot = getCurrentRoot()
		lastHash = block.Hash()
	}
	bc.Stop()

	expectedAccounts, expectedStorages := 3, int(stop) // test backend inserts 1 storage per block
	checkSnapshot(t, db, &expectedAccounts, &expectedStorages, false)

	// Great, now let's restart the chain
	cacheConfig.SnapshotNoBuild = true
	bc, err = core.NewBlockChain(
		db, &cacheConfig, g, engine, vm.Config{}, lastHash, false,
		core.Opts{LastAcceptedRoot: lastRoot},
	)
	require.NoError(t, err)
	bc.Validator().(*core.BlockValidator).CheckRoot = checkRootFn
	bc.InitializeSnapshots(&core.Opts{LastAcceptedRoot: lastRoot})

	start, stop = backend.BlockCount/2+1, backend.BlockCount
	for i := start; i <= stop; i++ {
		block := backend.GetBlock(i)
		t.Logf("Block: %d, Transactions: %d, Parent State: %x", i, len(block.Transactions()), lastRoot)

		// Override parentRoot to match last state
		parent := bc.GetHeaderByNumber(block.NumberU64() - 1)
		originalParentRoot := parent.Root
		parent.Root = lastRoot

		err := bc.InsertBlockManualWithParent(block, parent, true)
		require.NoError(t, err)

		// Restore parent root
		parent.Root = originalParentRoot

		t.Logf("Accepting block %s", block.Hash().Hex())
		err = bc.AcceptWithRoot(block, lastInsertedRoot)
		require.NoError(t, err)

		lastRoot = getCurrentRoot()
	}
	bc.Stop()

	expectedAccounts, expectedStorages = 3, int(stop) // test backend inserts 1 storage per block
	checkSnapshot(t, db, &expectedAccounts, &expectedStorages, false)
}

func checkSnapshot(t *testing.T, db ethdb.Database, expectedAccounts, expectedStorages *int, log bool) {
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
	if expectedAccounts != nil {
		require.Equal(t, *expectedAccounts, accounts)
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
	if expectedStorages != nil {
		require.Equal(t, *expectedStorages, storages)
	}
}
