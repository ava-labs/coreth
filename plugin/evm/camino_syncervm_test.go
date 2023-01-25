// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

func TestSkipStateSyncCamino(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 300, // must be greater than [syncableInterval] to skip sync
		shouldSync:         false,
	}
	vmSetup := createSyncServerAndClientVMsCamino(t, test)
	defer vmSetup.Teardown(t)

	testSyncerVM(t, vmSetup, test)
}

func createSyncServerAndClientVMsCamino(t *testing.T, test syncTest) *syncVMSetup {
	var (
		serverVM, syncerVM *VM
	)
	// If there is an error shutdown the VMs if they have been instantiated
	defer func() {
		// If the test has not already failed, shut down the VMs since the caller
		// will not get the chance to shut them down.
		if !t.Failed() {
			return
		}

		// If the test already failed, shut down the VMs if they were instantiated.
		if serverVM != nil {
			log.Info("Shutting down server VM")
			if err := serverVM.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if syncerVM != nil {
			log.Info("Shutting down syncerVM")
			if err := syncerVM.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}()

	// configure [serverVM]
	importAmount := 2000000 * units.Avax // 2M avax
	_, serverVM, _, serverAtomicMemory, serverAppSender := GenesisVMWithUTXOs(
		t,
		true,
		genesisJSONSunrisePhase0,
		"",
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	)

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

	// override serverAtomicTrie's commitInterval so the call to [serverAtomicTrie.Index]
	// creates a commit at the height [syncableInterval]. This is necessary to support
	// fetching a state summary.
	serverAtomicTrie := serverVM.atomicTrie.(*atomicTrie)
	serverAtomicTrie.commitInterval = test.syncableInterval
	assert.NoError(t, serverAtomicTrie.commit(test.syncableInterval, serverAtomicTrie.LastAcceptedRoot()))
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
	lastAccepted := serverVM.blockChain.LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.chaindb)
	blockBytes, err := rlp.EncodeToBytes(patchedBlock)
	if err != nil {
		t.Fatal(err)
	}
	internalBlock, err := serverVM.parseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}
	internalBlock.(*Block).SetStatus(choices.Accepted)
	assert.NoError(t, serverVM.State.SetLastAcceptedBlock(internalBlock))

	// patch syncableInterval for test
	serverVM.StateSyncServer.(*stateSyncServer).syncableInterval = test.syncableInterval

	// initialise [syncerVM] with blank genesis state
	stateSyncEnabledJSON := fmt.Sprintf("{\"state-sync-enabled\":true, \"state-sync-min-blocks\": %d}", test.stateSyncMinBlocks)
	syncerEngineChan, syncerVM, syncerDBManager, syncerAtomicMemory, syncerAppSender := GenesisVMWithUTXOs(
		t,
		false,
		"",
		stateSyncEnabledJSON,
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	)
	if err := syncerVM.SetState(context.Background(), snow.StateSyncing); err != nil {
		t.Fatal(err)
	}
	enabled, err := syncerVM.StateSyncEnabled(context.Background())
	assert.NoError(t, err)
	assert.True(t, enabled)

	// override [syncerVM]'s commit interval so the atomic trie works correctly.
	syncerVM.atomicTrie.(*atomicTrie).commitInterval = test.syncableInterval

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	serverAppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncerVM.AppResponse(ctx, nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncerVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	assert.NoError(t, syncerVM.Connected(
		context.Background(),
		serverVM.ctx.NodeID,
		statesyncclient.StateSyncVersion,
	))

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerAppSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go serverVM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}

	return &syncVMSetup{
		serverVM:        serverVM,
		serverAppSender: serverAppSender,
		includedAtomicTxs: []*Tx{
			importTx,
			exportTx,
		},
		fundedAccounts:     accounts,
		syncerVM:           syncerVM,
		syncerDBManager:    syncerDBManager,
		syncerEngineChan:   syncerEngineChan,
		syncerAtomicMemory: syncerAtomicMemory,
	}
}
