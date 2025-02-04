// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/testutils"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestEVMSyncerVM(t *testing.T) {
	for _, test := range testutils.SyncerVMTests {
		t.Run(test.Name, func(t *testing.T) {
			genFn := func(i int, vm extension.InnerVM, gen *core.BlockGen) {
				b, err := predicate.NewResults().Bytes()
				if err != nil {
					t.Fatal(err)
				}
				gen.AppendExtra(b)

				tx := types.NewTransaction(gen.TxNonce(testutils.TestEthAddrs[0]), testutils.TestEthAddrs[1], common.Big1, params.TxGas, testutils.InitialBaseFee, nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), testutils.TestKeys[0].ToECDSA())
				require.NoError(t, err)
				gen.AddTx(signedTx)
			}
			newVMFn := func() (extension.InnerVM, dummy.ConsensusCallbacks) {
				vm := newDefaultTestVM()
				return vm, vm.extensionConfig.ConsensusCallbacks
			}

			testSetup := &testutils.SyncTestSetup{
				NewVM:             newVMFn,
				GenFn:             genFn,
				ExtraSyncerVMTest: nil,
			}
			test.TestFunc(t, testSetup)
		})
	}
}
