package evm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/units"
	xmerkledb "github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/shim/merkledb"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
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

	logEach := 1_000
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
	t.Logf("Prefix: %x", prefix)
}

func TestReprocessGenesis(t *testing.T) {
	chainConfig := params.TestChainConfig
	testVM := &VM{
		chainConfig: chainConfig,
		codec:       Codec,
		ctx: &snow.Context{
			AVAXAssetID: ids.ID{1},
		},
	}
	key1, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	db := rawdb.NewMemoryDatabase()
	g := &core.Genesis{
		Config: chainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(1000000000000000000)}},
	}
	someAddr := common.Address{1}

	cbs := dummy.ConsensusCallbacks{
		OnExtraStateChange: func(block *types.Block, statedb *state.StateDB) (*big.Int, *big.Int, error) {
			i := block.Number().Uint64()
			statedb.SetNonce(someAddr, i)
			iBytes := binary.BigEndian.AppendUint64(nil, i)
			asHash := common.BytesToHash(iBytes)
			statedb.SetState(someAddr, asHash, asHash)
			return testVM.onExtraStateChange(block, statedb)
		},
	}
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

	engine := dummy.NewFakerWithMode(cbs, dummy.Mode{
		ModeSkipHeader: true,
	})
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
	// rawdb.WriteHeader(db, normalGenesis.Header())
	// bc.WriteHeadBlock(normalGenesis)
	require.NoError(t, bc.LoadGenesisState(normalGenesis))

	bc.InitializeSnapshots()

	t.Logf("Genesis block: %s", bc.CurrentBlock().Hash().Hex())
	getCurrentRoot := func() common.Hash {
		return cacheConfig.KeyValueDB.KVBackend.Root()
	}
	lastRoot := getCurrentRoot()

	// Let's generate some blocks
	signer := types.LatestSigner(chainConfig)
	_, blocks, _, err := core.GenerateChainWithGenesis(g, engine, 20, 2, func(i int, b *core.BlockGen) {
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: b.BaseFee(),
			Gas:      21000,
			To:       &addr1,
		}), signer, key1)
		b.AddTx(tx)
	})
	insertAfterRestart := blocks[10:]
	insertNow := blocks[:10]
	blocks = insertNow

	require.NoError(t, err)
	t.Logf("Generated %d blocks", len(blocks))
	for _, block := range blocks {
		t.Logf("Transactions: %d, Parent State: %x", len(block.Transactions()), lastRoot)

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

		bc.DrainAcceptorQueue()

		lastRoot = getCurrentRoot()
	}

	// Great, now let's try to stop and restart the chain
	bc.Stop()

	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	defer it.Release()
	for it.Next() {
		if len(it.Key()) != 33 {
			continue
		}
		t.Logf("Snapshot (account): %x, %x\n", it.Key(), it.Value())
	}

	it2 := db.NewIterator(rawdb.SnapshotStoragePrefix, nil)
	defer it2.Release()
	for it2.Next() {
		if len(it2.Key()) != 65 {
			continue
		}
		t.Logf("Snapshot (storage): %x, %x", it2.Key(), it2.Value())
	}

	lastAccepted := blocks[len(blocks)-1]
	cacheConfig.SnapshotNoBuild = true
	bc, err = core.NewBlockChain(
		db, &cacheConfig, g, engine, vm.Config{}, lastAccepted.Hash(), false,
		core.Opts{LastAcceptedRoot: lastRoot},
	)
	require.NoError(t, err)
	bc.Validator().(*core.BlockValidator).CheckRoot = checkRootFn
	bc.InitializeSnapshots(&core.Opts{LastAcceptedRoot: lastRoot})

	blocks = insertAfterRestart
	for _, block := range blocks {
		t.Logf("Transactions: %d, Parent State: %x", len(block.Transactions()), lastRoot)

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

		bc.DrainAcceptorQueue()

		lastRoot = getCurrentRoot()
	}
	bc.Stop()

	it = db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	defer it.Release()
	for it.Next() {
		if len(it.Key()) != 33 {
			continue
		}
		t.Logf("Snapshot (account): %x, %x\n", it.Key(), it.Value())
	}

	it2 = db.NewIterator(rawdb.SnapshotStoragePrefix, nil)
	defer it2.Release()
	for it2.Next() {
		if len(it2.Key()) != 65 {
			continue
		}
		t.Logf("Snapshot (storage): %x, %x", it2.Key(), it2.Value())
	}
}
