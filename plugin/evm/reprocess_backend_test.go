package evm

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database"
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
	CacheConfig core.CacheConfig
	VerifyRoot  bool
	Disk        ethdb.Database
	Metadata    database.Database
	Name        string

	mdb xmerkledb.MerkleDB
}

func (b *reprocessBackend) Close() error {
	return nil
}

func getMerkleDB(t *testing.T, mdbKVStore database.Database) xmerkledb.MerkleDB {
	ctx := context.Background()
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

	return mdb
}

func getCacheConfig(t *testing.T, name string, backend triedb.KVBackend) core.CacheConfig {
	cacheConfig := *core.DefaultCacheConfig
	cacheConfig.KeyValueDB = &triedb.KeyValueConfig{KVBackend: backend}
	cacheConfig.TriePrefetcherParallelism = prefetchers
	cacheConfig.SnapshotLimit = 0
	if useSnapshot {
		cacheConfig.SnapshotLimit = 256
	}
	cacheConfig.Pruning = pruning
	return cacheConfig
}

func getBackend(t *testing.T, name string, blocksCount int, dbs dbs) *reprocessBackend {
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
		ctx:         &snow.Context{AVAXAssetID: ids.ID{1}},
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
	_, blocks, _, err := core.GenerateChainWithGenesis(g, engine, blocksCount, 2, func(i int, b *core.BlockGen) {
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: b.BaseFee(),
			Gas:      21000,
			To:       &addr1,
		}), signer, key1)
		b.AddTx(tx)
	})
	require.NoError(t, err)
	require.Len(t, blocks, blocksCount)

	var (
		merkleDB  xmerkledb.MerkleDB
		kvBackend triedb.KVBackend
	)
	if name == "merkledb" {
		merkleDB = getMerkleDB(t, dbs.merkledb)
		kvBackend = merkledb.NewMerkleDB(merkleDB)
	}
	return &reprocessBackend{
		Genesis:     g,
		Engine:      engine,
		GetBlock:    func(i uint64) *types.Block { return blocks[i-1] },
		CacheConfig: getCacheConfig(t, name, kvBackend),
		Disk:        dbs.chain,
		Metadata:    dbs.metadata,
		Name:        name,
		VerifyRoot:  name == "legacy",
		mdb:         merkleDB,
	}
}

func getMainnetBackend(t *testing.T, name string, source ethdb.Database, dbs dbs) *reprocessBackend {
	var g core.Genesis
	require.NoError(t, json.Unmarshal([]byte(cChainGenesisMainnet), &g))

	testVM := &VM{
		chainConfig: g.Config,
		codec:       Codec,
		ctx:         &snow.Context{AVAXAssetID: mainnetAvaxAssetID},
	}
	cbs := dummy.ConsensusCallbacks{OnExtraStateChange: testVM.onExtraStateChange}
	engine := dummy.NewFakerWithMode(cbs, dummy.Mode{ModeSkipHeader: true})

	var (
		merkleDB  xmerkledb.MerkleDB
		kvBackend triedb.KVBackend
	)
	if name == "merkledb" {
		merkleDB = getMerkleDB(t, dbs.merkledb)
		kvBackend = merkledb.NewMerkleDB(merkleDB)
	}
	return &reprocessBackend{
		Genesis: &g,
		Engine:  engine,
		GetBlock: func(i uint64) *types.Block {
			hash := rawdb.ReadCanonicalHash(source, i)
			block := rawdb.ReadBlock(source, hash, i)
			require.NotNil(t, block)
			return block
		},
		CacheConfig: getCacheConfig(t, name, kvBackend),
		Disk:        dbs.chain,
		Metadata:    dbs.metadata,
		Name:        name,
		VerifyRoot:  name == "legacy",
		mdb:         merkleDB,
	}
}
