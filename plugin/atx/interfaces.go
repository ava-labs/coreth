package atx

import (
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

type StateDB interface {
	SubBalance(common.Address, *big.Int)
	AddBalance(common.Address, *big.Int)
	GetBalance(common.Address) *big.Int

	GetBalanceMultiCoin(common.Address, common.Hash) *big.Int
	SubBalanceMultiCoin(common.Address, common.Hash, *big.Int)
	AddBalanceMultiCoin(common.Address, common.Hash, *big.Int)

	GetNonce(common.Address) uint64
	SetNonce(common.Address, uint64)

	Snapshot() int
	RevertToSnapshot(int)
}

type BlockChain interface {
	State() (StateDB, error)
	StateAt(root common.Hash) (StateDB, error)
	CurrentHeader() *types.Header
	HasBlock(common.Hash, uint64) bool
}

type BlockGetter interface {
	GetBlockAndAtomicTxs(ids.ID) (uint64, []*Tx, choices.Status, ids.ID, error)
}
