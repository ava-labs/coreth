// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ethereum/go-ethereum/common"
)

func TestSyncerVMReturnsStateSyncLastSummary(t *testing.T) {
	tests := []struct {
		name             string
		syncableInterval uint64
		blocksToBuild    int
		minBlocks        uint64
		expectedMessage  commonEng.Message
	}{
		{
			name:             "state sync skipped",
			syncableInterval: core.CommitInterval,
			blocksToBuild:    100,
			minBlocks:        1000,
			expectedMessage:  commonEng.StateSyncSkipped,
		},
		{
			name:             "state sync performed",
			syncableInterval: core.CommitInterval,      // must be multiple of [core.CommitInterval]
			blocksToBuild:    core.CommitInterval + 50, // must be greater than [syncableInterval]
			minBlocks:        core.CommitInterval - 50, // arbitrary choice less than [syncableInterval]
			expectedMessage:  commonEng.StateSyncDone,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testSyncerVM(t, test.blocksToBuild, test.minBlocks, test.syncableInterval, test.expectedMessage)
		})
	}
}

func testSyncerVM(t *testing.T, blocksToBuild int, minBlocks uint64, syncableInterval uint64, expectedMessage commonEng.Message) {
	importAmount := 2000000 * units.Avax // 2M avax
	issuer, syncedVM, _, _, syncedVMAppSender := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	syncedVMNodeID := ids.GenerateTestShortID()

	defer func() {
		if err := syncedVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	syncedVM.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key, err := keystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	importTx, err := syncedVM.newImportTx(syncedVM.ctx.XChainID, key.Address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := syncedVM.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk1, err := syncedVM.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk1.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := syncedVM.SetPreference(blk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk1.Accept(); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk1.ID()) {
		t.Fatalf("Expected new block to match")
	}

	keys := make([]*keystore.Key, 10)
	for i := 0; i < 10; i++ {
		keys[i], err = keystore.NewKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
	}

	nonce := uint64(0)
	for i := 0; i < blocksToBuild; i++ {
		txs := make([]*types.Transaction, 10)
		for i := 0; i < 10; i++ {
			tx := types.NewTransaction(nonce, keys[i].Address, big.NewInt(1), 21000, big.NewInt(params.ApricotPhase1MinGasPrice), nil)
			nonce++
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(syncedVM.chainID), key.PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			txs[i] = signedTx
		}
		errs := syncedVM.chain.AddRemoteTxsSync(txs)
		for i, err := range errs {
			if err != nil {
				t.Fatalf("Failed to add tx at index %d: %s", i, err)
			}
		}

		<-issuer

		blk2, err := syncedVM.BuildBlock()
		if err != nil {
			t.Fatal(err)
		}

		if err := blk2.Verify(); err != nil {
			t.Fatal(err)
		}

		if status := blk2.Status(); status != choices.Processing {
			t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
		}

		if err := blk2.Accept(); err != nil {
			t.Fatal(err)
		}

		newHead = <-newTxPoolHeadChan
		if newHead.Head.Hash() != common.Hash(blk2.ID()) {
			t.Fatalf("Expected new block to match")
		}

		if status := blk2.Status(); status != choices.Accepted {
			t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
		}
	}

	// patch syncableInterval for test
	syncedVM.stateSyncer.syncableInterval = syncableInterval

	summary, err := syncedVM.StateSyncGetLastSummary()
	if err != nil {
		t.Fatal("error getting state sync last summary", "err", err)
	}
	parsedSummary, err := syncedVM.StateSyncParseSummary(summary.Bytes())
	if err != nil {
		t.Fatal("error getting state sync last summary", "err", err)
	}
	retrievedSummary, err := syncedVM.StateSyncGetSummary(parsedSummary.Key())
	if err != nil {
		t.Fatal("error when checking if summary is accepted", "err", err)
	}
	assert.Equal(t, summary, retrievedSummary)

	// initialise stateSyncVM with blank genesis state
	stateSyncEngineChan, stateSyncVM, _, _, stateSyncAppSender := GenesisVM(t, false, genesisJSONApricotPhase2, "{\"state-sync-enabled\":true}", "")
	enabled, err := stateSyncVM.StateSyncEnabled()
	assert.NoError(t, err)
	assert.True(t, enabled)

	// override syncedVM's SendAppResponse function such that it triggers AppResponse on
	// the stateSyncVM
	syncedVMAppSender.CantSendAppResponse = true
	syncedVMAppSender.SendAppResponseF = func(nodeID ids.ShortID, requestID uint32, response []byte) error {
		go stateSyncVM.AppResponse(nodeID, requestID, response)
		return nil
	}

	// connect peer to stateSyncVM
	assert.NoError(t, stateSyncVM.SetState(snow.StateSyncing))
	assert.NoError(t, stateSyncVM.Connected(syncedVMNodeID, syncclient.StateSyncVersion))

	// override stateSyncVM's SendAppRequest function such that it triggers AppRequest on
	// the syncedVM
	stateSyncAppSender.CantSendAppRequest = true
	stateSyncAppSender.SendAppRequestF = func(nodeSet ids.ShortSet, requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go syncedVM.AppRequest(nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}

	// patch minBlocks for test
	stateSyncVM.minBlocks = minBlocks

	// set VM state to state syncing
	err = stateSyncVM.StateSync([]commonEng.Summary{summary})
	if err != nil {
		t.Fatal("unexpected error when initiating state sync")
	}
	msg := <-stateSyncEngineChan
	assert.Equal(t, expectedMessage, msg)
	if expectedMessage == commonEng.StateSyncSkipped {
		// if state sync should be skipped, don't expect
		// the state to have been updated and return early
		return
	}

	blockID, syncedHeight, err := stateSyncVM.StateSyncGetResult()
	if err != nil {
		t.Fatal("state sync failed", err)
	}

	blk, err := syncedVM.GetBlock(blockID)
	if err != nil {
		t.Fatal("error getting block", blockID, err)
	}
	if blk.Height() != syncedHeight {
		t.Fatalf("Expected block height to be %d, but found %d", syncedHeight, blk.Height())
	}

	assert.NoError(t, stateSyncVM.StateSyncSetLastSummaryBlock(blk.Bytes()))

	assert.NoError(t, stateSyncVM.SetState(snow.Bootstrapping))

	stateSyncVMHeight := stateSyncVM.LastAcceptedBlock().Height()
	lastAcceptedHeight := syncedVM.LastAcceptedBlock().Height()
	// Assert that the [stateSyncVMHeight] matches the most recent commit.
	expectedCommitHeight := lastAcceptedHeight - (lastAcceptedHeight % core.CommitInterval)
	assert.Equal(t, expectedCommitHeight, stateSyncVMHeight)
	assert.Equal(t, expectedCommitHeight, syncedHeight)

	blkID, err := syncedVM.LastAccepted()
	if err != nil {
		t.Fatal("error getting last accepted block ID", err)
	}

	blks := make([]snowman.Block, lastAcceptedHeight-stateSyncVMHeight)
	for i := len(blks) - 1; i >= 0; i-- {
		blk, err = syncedVM.getBlock(blkID)
		if err != nil {
			t.Fatal("error getting block", err)
		}
		blks[i] = blk
		blkID = blk.Parent()
	}

	for _, blk := range blks {
		blk, err := stateSyncVM.ParseBlock(blk.Bytes())
		if err != nil {
			t.Fatal("error parsing block", err)
		}

		if err = blk.Verify(); err != nil {
			t.Fatal("error verifying block", err)
		}

		if err = blk.Accept(); err != nil {
			t.Fatal("error accepting block", err)
		}
	}

	assert.Equal(t, lastAcceptedHeight, stateSyncVM.LastAcceptedBlock().Height(), "%d!=%d")
	assert.NoError(t, stateSyncVM.SetState(snow.NormalOp))
	assert.True(t, stateSyncVM.bootstrapped)
}
