package evm

import (
	"context"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/units"
	xmerkledb "github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/shim/merkledb"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type reprocessBackend struct {
	Genesis     *core.Genesis
	Engine      consensus.Engine
	GetBlock    func(uint64) *types.Block
	BlockCount  uint64
	CacheConfig core.CacheConfig
	VerifyRoot  bool
	Disk        ethdb.Database
	Name        string
}

func getCacheConfig(t *testing.T, name string) core.CacheConfig {
	var backend triedb.KVBackend
	if name == "merkledb" {
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
		backend = merkledb.NewMerkleDB(mdb)
	}

	cacheConfig := *core.DefaultCacheConfig
	cacheConfig.KeyValueDB = &triedb.KeyValueConfig{KVBackend: backend}
	cacheConfig.TriePrefetcherParallelism = 4
	cacheConfig.SnapshotLimit = 256
	cacheConfig.SnapshotDelayInit = true
	// cacheConfig.Pruning = false
	return cacheConfig
}

func getBackend(t *testing.T, name string) *reprocessBackend {
	chainConfig := params.TestChainConfig
	key1, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	g := &core.Genesis{
		Config: chainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(1000000000000000000)}},
	}
	testVM := &VM{
		chainConfig: chainConfig,
		codec:       Codec,
		ctx: &snow.Context{
			AVAXAssetID: ids.ID{1},
		},
	}
	someAddr := common.Address{1}

	endOfBlockStateTransition := func(block *types.Header, statedb *state.StateDB) {
		i := block.Number.Uint64()
		statedb.SetNonce(someAddr, i)
		iBytes := binary.BigEndian.AppendUint64(nil, i)
		asHash := common.BytesToHash(iBytes)
		statedb.SetState(someAddr, asHash, asHash)
	}

	cbs := dummy.ConsensusCallbacks{
		OnExtraStateChange: func(block *types.Block, statedb *state.StateDB) (*big.Int, *big.Int, error) {
			endOfBlockStateTransition(block.Header(), statedb)
			return testVM.onExtraStateChange(block, statedb)
		},
		OnFinalizeAndAssemble: func(header *types.Header, state *state.StateDB, txs []*types.Transaction) (extraData []byte, blockFeeContribution *big.Int, extDataGasUsed *big.Int, err error) {
			endOfBlockStateTransition(header, state)
			return nil, nil, nil, nil
		},
	}

	engine := dummy.NewFakerWithMode(cbs, dummy.Mode{
		ModeSkipHeader: true,
	})

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
	require.NoError(t, err)

	return &reprocessBackend{
		Genesis:     g,
		Engine:      engine,
		BlockCount:  uint64(len(blocks)),
		GetBlock:    func(i uint64) *types.Block { return blocks[i-1] },
		CacheConfig: getCacheConfig(t, name),
		Disk:        rawdb.NewMemoryDatabase(),
		Name:        name,
	}
}
