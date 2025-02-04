package testutils

import (
	"encoding/json"
	"math/big"
	"testing"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow"
	commoneng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
)

var (
	GenesisJSONApricotPhase0     = genesisJSON(params.TestLaunchConfig)
	GenesisJSONApricotPhase1     = genesisJSON(params.TestApricotPhase1Config)
	GenesisJSONApricotPhase2     = genesisJSON(params.TestApricotPhase2Config)
	GenesisJSONApricotPhase3     = genesisJSON(params.TestApricotPhase3Config)
	GenesisJSONApricotPhase4     = genesisJSON(params.TestApricotPhase4Config)
	GenesisJSONApricotPhase5     = genesisJSON(params.TestApricotPhase5Config)
	GenesisJSONApricotPhasePre6  = genesisJSON(params.TestApricotPhasePre6Config)
	GenesisJSONApricotPhase6     = genesisJSON(params.TestApricotPhase6Config)
	GenesisJSONApricotPhasePost6 = genesisJSON(params.TestApricotPhasePost6Config)
	GenesisJSONBanff             = genesisJSON(params.TestBanffChainConfig)
	GenesisJSONCortina           = genesisJSON(params.TestCortinaChainConfig)
	GenesisJSONDurango           = genesisJSON(params.TestDurangoChainConfig)
	GenesisJSONEtna              = genesisJSON(params.TestEtnaChainConfig)

	GenesisJSONLatest = GenesisJSONEtna
)

// genesisJSON returns the JSON representation of the genesis block
// for the given chain configuration, with pre-funded accounts.
func genesisJSON(cfg *params.ChainConfig) string {
	g := new(core.Genesis)
	g.Difficulty = big.NewInt(0)
	g.GasLimit = 0x5f5e100
	g.Timestamp = uint64(upgrade.InitiallyActiveTime.Unix())

	// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
	// config overridden in vm.Initialize.
	cpy := *cfg
	cpy.ChainID = big.NewInt(43111)
	g.Config = &cpy

	allocStr := `{"0100000000000000000000000000000000000000":{"code":"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033","balance":"0x0"}}`
	json.Unmarshal([]byte(allocStr), &g.Alloc)
	// After Durango, an additional account is funded in tests to use
	// with warp messages.
	if cfg.IsDurango(0) {
		addr := common.HexToAddress("0x99b9DEA54C48Dfea6aA9A4Ca4623633EE04ddbB5")
		balance := new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(10))
		g.Alloc[addr] = types.GenesisAccount{Balance: balance}
	}

	// Fund the test keys
	for _, addr := range TestEthAddrs {
		balance := new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(10))
		g.Alloc[addr] = types.GenesisAccount{Balance: balance}
	}

	b, err := json.Marshal(g)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func NewPrefundedGenesis(
	balance int,
	addresses ...common.Address,
) *core.Genesis {
	alloc := types.GenesisAlloc{}
	for _, address := range addresses {
		alloc[address] = types.GenesisAccount{
			Balance: big.NewInt(int64(balance)),
		}
	}

	return &core.Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		Alloc:      alloc,
	}
}

// SetupGenesis sets up the genesis
// If [genesisJSON] is empty, defaults to using [genesisJSONLatest]
func SetupGenesis(
	t *testing.T,
	genesisJSON string,
) (*snow.Context,
	database.Database,
	[]byte,
	chan commoneng.Message,
	*avalancheatomic.Memory,
) {
	genesisBytes := []byte(genesisJSON)
	ctx := utils.TestSnowContext()

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := avalancheatomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	issuer := make(chan commoneng.Message, 1)
	prefixedDB := prefixdb.New([]byte{1}, baseDB)
	return ctx, prefixedDB, genesisBytes, issuer, atomicMemory
}
