// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
)

type vmFixture struct {
	require *require.Assertions
	vm      *VM
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

func createVMFixture(configJSON string, upgradeJSON string, prefundedEthAddrs map[common.Address]uint64, prefundedUTXOs map[ids.ShortID]uint64) *vmFixture {
	require := require.New(ginkgo.GinkgoT())

	genesis := newPrefundedGenesis(prefundedEthAddrs)
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(err)

	_, vm, _, _, _ := GenesisVMWithUTXOs(ginkgo.GinkgoT(), true, string(genesisBytes), configJSON, upgradeJSON, prefundedUTXOs)
	return &vmFixture{
		vm:      vm,
		require: require,
	}
}

func (v *vmFixture) IssueTxs(txs []*types.Transaction) {
	errs := v.vm.GetTxpool().AddRemotesSync(txs)
	for _, err := range errs {
		v.require.NoError(err)
	}
}

func (v *vmFixture) IssueAtomicTxs(atomicTxs []*Tx) {
	mempool := v.vm.GetAtomicMempool()
	for _, tx := range atomicTxs {
		v.require.NoError(mempool.AddTx(tx))
	}
}

func (v *vmFixture) BuildAndAccept() {
	ctx := context.Background()
	block, err := v.vm.BuildBlock(ctx)
	v.require.NoError(err)

	v.require.NoError(block.Verify(ctx))
	v.require.NoError(v.vm.SetPreference(ctx, block.ID()))
	v.require.NoError(block.Accept(ctx))
}

func (v *vmFixture) Teardown() {
	v.require.NoError(v.vm.Shutdown(context.Background()))
}
