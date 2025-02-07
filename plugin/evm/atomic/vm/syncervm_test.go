// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/testutils"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAtomicSyncerVM(t *testing.T) {
	importAmount := 2000000 * units.Avax // 2M avax
	for _, test := range testutils.SyncerVMTests {
		includedAtomicTxs := make([]*atomic.Tx, 0)

		t.Run(test.Name, func(t *testing.T) {
			genFn := func(i int, vm extension.InnerVM, gen *core.BlockGen) {
				atomicVM, ok := vm.(*VM)
				require.True(t, ok)
				b, err := predicate.NewResults().Bytes()
				if err != nil {
					t.Fatal(err)
				}
				gen.AppendExtra(b)
				switch i {
				case 0:
					// spend the UTXOs from shared memory
					importTx, err := atomicVM.newImportTx(atomicVM.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, testutils.TestKeys[0:1])
					require.NoError(t, err)
					require.NoError(t, atomicVM.mempool.AddLocalTx(importTx))
					includedAtomicTxs = append(includedAtomicTxs, importTx)
				case 1:
					// export some of the imported UTXOs to test exportTx is properly synced
					state, err := vm.Ethereum().BlockChain().State()
					if err != nil {
						t.Fatal(err)
					}
					exportTx, err := atomic.NewExportTx(
						atomicVM.ctx,
						atomicVM.CurrentRules(),
						state,
						atomicVM.ctx.AVAXAssetID,
						importAmount/2,
						atomicVM.ctx.XChainID,
						testutils.TestShortIDAddrs[0],
						testutils.InitialBaseFee,
						testutils.TestKeys[0:1],
					)
					require.NoError(t, err)
					require.NoError(t, atomicVM.mempool.AddLocalTx(exportTx))
					includedAtomicTxs = append(includedAtomicTxs, exportTx)
				default: // Generate simple transfer transactions.
					pk := testutils.TestKeys[0].ToECDSA()
					tx := types.NewTransaction(gen.TxNonce(testutils.TestEthAddrs[0]), testutils.TestEthAddrs[1], common.Big1, params.TxGas, testutils.InitialBaseFee, nil)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), pk)
					require.NoError(t, err)
					gen.AddTx(signedTx)
				}
			}
			newVMFn := func() (extension.InnerVM, dummy.ConsensusCallbacks) {
				vm := newAtomicTestVM()
				return vm, vm.createConsensusCallbacks()
			}

			afterInit := func(t *testing.T, params testutils.SyncTestParams, vmSetup testutils.VMSetup, isServer bool) {
				atomicVM, ok := vmSetup.VM.(*VM)
				require.True(t, ok)

				alloc := map[ids.ShortID]uint64{
					testutils.TestShortIDAddrs[0]: importAmount,
				}

				for addr, avaxAmount := range alloc {
					txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
					if err != nil {
						t.Fatalf("Failed to generate txID from addr: %s", err)
					}
					if _, err := addUTXO(vmSetup.AtomicMemory, vmSetup.SnowCtx, txID, 0, vmSetup.SnowCtx.AVAXAssetID, avaxAmount, addr); err != nil {
						t.Fatalf("Failed to add UTXO to shared memory: %s", err)
					}
				}
				if isServer {
					serverAtomicTrie := atomicVM.atomicBackend.AtomicTrie()
					// Calling AcceptTrie with SyncableInterval creates a commit for the atomic trie
					committed, err := serverAtomicTrie.AcceptTrie(params.SyncableInterval, serverAtomicTrie.LastAcceptedRoot())
					require.NoError(t, err)
					require.True(t, committed)
					require.NoError(t, atomicVM.VersionDB().Commit())
				}
			}

			testSetup := &testutils.SyncTestSetup{
				NewVM:     newVMFn,
				GenFn:     genFn,
				AfterInit: afterInit,
				ExtraSyncerVMTest: func(t *testing.T, syncerVMSetup testutils.VMSetup) {
					// check atomic memory was synced properly
					syncerVM := syncerVMSetup.VM
					atomicVM, ok := syncerVM.(*VM)
					require.True(t, ok)
					syncerSharedMemories := atomictest.NewSharedMemories(syncerVMSetup.AtomicMemory, atomicVM.ctx.ChainID, atomicVM.ctx.XChainID)

					for _, tx := range includedAtomicTxs {
						atomicOps, err := atomictest.ConvertToAtomicOps(tx)
						require.NoError(t, err)
						syncerSharedMemories.AssertOpsApplied(t, atomicOps)
					}
				},
			}
			test.TestFunc(t, testSetup)
		})
	}
}
