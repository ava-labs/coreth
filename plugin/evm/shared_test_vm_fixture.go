// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	engCommon "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

type vmFixture struct {
	require *require.Assertions
	Issuer  chan engCommon.Message
	VM      *VM
}

func newPrefundedGenesis(
	prefundedEthAddrs map[common.Address]uint64,
) *core.Genesis {
	alloc := core.GenesisAlloc{}
	for address, balance := range prefundedEthAddrs {
		alloc[address] = core.GenesisAccount{
			Balance: big.NewInt(int64(balance)),
		}
	}

	return &core.Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		Alloc:      alloc,
	}
}

func CreateVMFixture(t require.TestingT, prefundedUTXOs map[ids.ShortID]uint64) *vmFixture {
	require := require.New(t)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", prefundedUTXOs)
	return &vmFixture{
		VM:      vm,
		Issuer:  issuer,
		require: require,
	}
}

func (v *vmFixture) IssueTxs(txs []*types.Transaction) {
	errs := v.VM.GetTxpool().AddRemotesSync(txs)
	for _, err := range errs {
		v.require.NoError(err)
	}
}

func (v *vmFixture) IssueAtomicTxs(atomicTxs []*Tx) {
	mempool := v.VM.GetAtomicMempool()
	for _, tx := range atomicTxs {
		v.require.NoError(mempool.AddTx(tx))
	}
}

func (v *vmFixture) BuildAndAccept() {
	ctx := context.Background()
	block, err := v.VM.BuildBlock(ctx)
	v.require.NoError(err)

	v.require.NoError(block.Verify(ctx))
	v.require.NoError(v.VM.SetPreference(ctx, block.ID()))
	v.require.NoError(block.Accept(ctx))
}

func (v *vmFixture) Teardown() {
	v.require.NoError(v.VM.Shutdown(context.Background()))
}
