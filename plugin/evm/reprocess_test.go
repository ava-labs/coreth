package evm

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

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
			i := byte(block.Number().Uint64())
			statedb.SetNonce(someAddr, uint64(i))
			statedb.SetState(someAddr, common.Hash{i}, common.Hash{i})
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
	bc.Validator().(*core.BlockValidator).CheckRoot = func(expected, got common.Hash) bool {
		t.Logf("Got root: %s", got.Hex())
		lastInsertedRoot = got
		return true
	}

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
	_, blocks, _, err := core.GenerateChainWithGenesis(g, engine, 10, 2, func(i int, b *core.BlockGen) {
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: b.BaseFee(),
			Gas:      21000,
			To:       &addr1,
		}), signer, key1)
		b.AddTx(tx)
	})
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
}
