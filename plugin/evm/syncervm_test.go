// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"

	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/params"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestSyncerVME2E(t *testing.T) {
	tests := []struct {
		name             string
		syncableInterval uint64
		minBlocks        uint64
		expectedMessage  commonEng.Message
	}{
		{
			name:             "state sync skipped",
			syncableInterval: 256,
			minBlocks:        300, // must be greater than [syncableInterval] to skip sync
			expectedMessage:  commonEng.StateSyncSkipped,
		},
		{
			name:             "state sync performed",
			syncableInterval: 256,
			minBlocks:        50, // must be less than [syncableInterval] to perform sync
			expectedMessage:  commonEng.StateSyncDone,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rand.Seed(1)
			testSyncerVM(t, test.minBlocks, test.syncableInterval, test.expectedMessage)
		})
	}
}

func testSyncerVM(t *testing.T, minBlocks uint64, syncableInterval uint64, expectedMessage commonEng.Message) {
	// configure [serverVM]
	importAmount := 2000000 * units.Avax // 2M avax
	_, serverVM, _, serverAtomicMemory, serverAppSender := GenesisVMWithUTXOs(
		t,
		true,
		genesisJSONApricotPhase5,
		"",
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	)
	defer func() {
		if err := serverVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	var (
		importTx, exportTx *Tx
		err                error
	)
	generateAndAcceptBlocks(t, serverVM, parentsToGet, func(i int, gen *core.BlockGen) {
		switch i {
		case 0:
			// spend the UTXOs from shared memory
			importTx, err = serverVM.newImportTx(serverVM.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}
			if err := serverVM.issueTx(importTx, true /*=local*/); err != nil {
				t.Fatal(err)
			}
		case 1:
			// export some of the imported UTXOs to test exportTx is properly synced
			exportTx, err = serverVM.newExportTx(
				serverVM.ctx.AVAXAssetID,
				importAmount/2,
				serverVM.ctx.XChainID,
				testShortIDAddrs[0],
				initialBaseFee,
				[]*crypto.PrivateKeySECP256K1R{testKeys[0]},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := serverVM.issueTx(exportTx, true /*=local*/); err != nil {
				t.Fatal(err)
			}
		default: // Generate simple transfer transactions.
			pk := testKeys[0].ToECDSA()
			tx := types.NewTransaction(gen.TxNonce(testEthAddrs[0]), testEthAddrs[1], common.Big1, params.TxGas, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainID), pk)
			if err != nil {
				t.Fatal(t)
			}
			gen.AddTx(signedTx)
		}
	})

	// override atomicTrie's commitHeightInterval so the call to [atomicTrie.Index]
	// creates a commit at the height [syncableInterval]. This is necessary to support
	// fetching a state summary.
	serverVM.atomicTrie.(*atomicTrie).commitHeightInterval = syncableInterval
	assert.NoError(t, serverVM.atomicTrie.Index(syncableInterval, nil))
	assert.NoError(t, serverVM.db.Commit())

	serverSharedMemories := newSharedMemories(serverAtomicMemory, serverVM.ctx.ChainID, serverVM.ctx.XChainID)
	serverSharedMemories.assertOpsApplied(t, importTx.mustAtomicOps())
	serverSharedMemories.assertOpsApplied(t, exportTx.mustAtomicOps())

	// make some accounts
	trieDB := trie.NewDatabase(serverVM.chaindb)
	root, accounts := statesync.FillAccountsWithOverlappingStorage(t, trieDB, types.EmptyRootHash, 1000, 16)

	// patch serverVM's lastAcceptedBlock to have the new root
	// and update the vm's state so the trie with accounts will
	// be returned by StateSyncGetLastSummary
	lastAccepted := serverVM.chain.LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.chaindb)
	assert.NoError(t, serverVM.initChainState(patchedBlock, false))

	// patch syncableInterval for test
	serverVM.stateSyncer.syncableInterval = syncableInterval

	// get last summary and test related methods
	summary, err := serverVM.StateSyncGetLastSummary()
	if err != nil {
		t.Fatal("error getting state sync last summary", "err", err)
	}
	parsedSummary, err := serverVM.StateSyncParseSummary(summary.Bytes())
	if err != nil {
		t.Fatal("error getting state sync last summary", "err", err)
	}
	retrievedSummary, err := serverVM.StateSyncGetSummary(parsedSummary.Key())
	if err != nil {
		t.Fatal("error when checking if summary is accepted", "err", err)
	}
	assert.Equal(t, summary, retrievedSummary)

	// initialise [syncerVM] with blank genesis state
	stateSyncEnabledJSON := "{\"state-sync-enabled\":true}"
	syncerEngineChan, syncerVM, _, syncerAtomicMemory, syncerAppSender := GenesisVMWithUTXOs(
		t,
		true,
		genesisJSONApricotPhase5,
		stateSyncEnabledJSON,
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	)
	defer func() {
		if err := syncerVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	enabled, err := syncerVM.StateSyncEnabled()
	assert.NoError(t, err)
	assert.True(t, enabled)

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	serverAppSender.CantSendAppResponse = true
	serverAppSender.SendAppResponseF = func(nodeID ids.ShortID, requestID uint32, response []byte) error {
		go syncerVM.AppResponse(nodeID, requestID, response)
		return nil
	}

	// connect peer to [syncerVM]
	assert.NoError(t, syncerVM.Connected(serverVM.ctx.NodeID, statesyncclient.StateSyncVersion))

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerAppSender.CantSendAppRequest = true
	syncerAppSender.SendAppRequestF = func(nodeSet ids.ShortSet, requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go serverVM.AppRequest(nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}

	// patch minBlocks for test
	syncerVM.minBlocks = minBlocks

	// set [syncerVM] state to state syncing & perform state sync
	assert.NoError(t, syncerVM.SetState(snow.StateSyncing))
	err = syncerVM.StateSync([]commonEng.Summary{summary})
	if err != nil {
		t.Fatal("unexpected error when initiating state sync")
	}
	msg := <-syncerEngineChan
	assert.Equal(t, expectedMessage, msg)
	if expectedMessage == commonEng.StateSyncSkipped {
		// if state sync should be skipped, don't expect
		// the state to have been updated and return early
		return
	}

	// verify state sync completed with the expected height and blockID
	blockID, syncedHeight, err := syncerVM.StateSyncGetResult()
	if err != nil {
		t.Fatal("state sync failed", err)
	}
	assert.Equal(t, serverVM.LastAcceptedBlock().Height(), syncedHeight)

	// get the last block from [serverVM] and call [syncerVM.SetLastSummaryBlock]
	// as the final step
	blk, err := serverVM.GetBlock(blockID)
	if err != nil {
		t.Fatal("error getting block", blockID, err)
	}
	if blk.Height() != syncedHeight {
		t.Fatalf("Expected block height to be %d, but found %d", syncedHeight, blk.Height())
	}
	assert.NoError(t, syncerVM.StateSyncSetLastSummaryBlock(blk.Bytes()))
	assert.Equal(t, serverVM.LastAcceptedBlock().Height(), syncerVM.LastAcceptedBlock().Height())

	// set [syncerVM] to bootstrapping and verify we can process some blocks
	assert.NoError(t, syncerVM.SetState(snow.Bootstrapping))
	blocksToBuild := 10
	txsPerBlock := 10
	toAddress := testEthAddrs[2] // arbitrary choice
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		i := 0
		for k := range accounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainID), k.PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	})

	// check we can transition to [NormalOp] state
	assert.NoError(t, syncerVM.SetState(snow.NormalOp))
	assert.True(t, syncerVM.bootstrapped)

	// check atomic memory was synced properly
	syncerSharedMemories := newSharedMemories(syncerAtomicMemory, syncerVM.ctx.ChainID, syncerVM.ctx.XChainID)
	syncerSharedMemories.assertOpsApplied(t, importTx.mustAtomicOps())
	syncerSharedMemories.assertOpsApplied(t, exportTx.mustAtomicOps())
}

// patchBlock returns a copy of [blk] with [root] and updates [db] to
// include the new block as canonical for [blk]'s height.
// This breaks the digestibility of the chain since after this call
// [blk] does not necessarily define a state transition from its parent
// state to the new state root.
func patchBlock(blk *types.Block, root common.Hash, db ethdb.Database) *types.Block {
	header := blk.Header()
	header.Root = root
	receipts := rawdb.ReadRawReceipts(db, blk.Hash(), blk.NumberU64())
	newBlk := types.NewBlock(
		header, blk.Transactions(), blk.Uncles(), receipts, trie.NewStackTrie(nil), blk.ExtData(), true,
	)
	rawdb.WriteBlock(db, newBlk)
	rawdb.WriteCanonicalHash(db, newBlk.Hash(), newBlk.NumberU64())
	return newBlk
}

// generateAndAcceptBlocks uses [core.GenerateChain] to generate blocks, then
// calls Verify and Accept on each generated block
// TODO: consider using this helper function in vm_test.go and elsewhere in this package to clean up tests
func generateAndAcceptBlocks(t *testing.T, vm *VM, numBlocks int, gen func(int, *core.BlockGen)) {
	t.Helper()

	// acceptExternalBlock defines a function to parse, verify, and accept a block once it has been
	// generated by GenerateChain
	acceptExternalBlock := func(block *types.Block) {
		bytes, err := rlp.EncodeToBytes(block)
		if err != nil {
			t.Fatal(err)
		}
		vmBlock, err := vm.ParseBlock(bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Verify(); err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Accept(); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := core.GenerateChain(
		vm.chainConfig,
		vm.chain.LastAcceptedBlock(),
		dummy.NewDummyEngine(vm.createConsensusCallbacks()),
		vm.chaindb,
		numBlocks,
		10,
		func(i int, g *core.BlockGen) {
			g.SetOnFinish(acceptExternalBlock)
			g.SetCoinbase(coreth.BlackholeAddr) // necessary for syntactic validation of the block
			gen(i, g)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}
