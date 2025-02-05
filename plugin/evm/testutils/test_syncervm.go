// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/plugin/evm/database"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	vmsync "github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ava-labs/coreth/predicate"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

type SyncerVMTest struct {
	Name     string
	TestFunc func(
		t *testing.T,
		testSetup *SyncTestSetup,
	)
}

var SyncerVMTests = []SyncerVMTest{
	{
		Name:     "SkipStateSyncTest",
		TestFunc: SkipStateSyncTest,
	},
	{
		Name:     "StateSyncFromScratchTest",
		TestFunc: StateSyncFromScratchTest,
	},
	{
		Name:     "StateSyncFromScratchExceedParentTest",
		TestFunc: StateSyncFromScratchExceedParentTest,
	},
	{
		Name:     "StateSyncToggleEnabledToDisabledTest",
		TestFunc: StateSyncToggleEnabledToDisabledTest,
	},
	{
		Name:     "VMShutdownWhileSyncingTest",
		TestFunc: VMShutdownWhileSyncingTest,
	},
}

func SkipStateSyncTest(t *testing.T, testSetup *SyncTestSetup) {
	rand.Seed(1)
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 300, // must be greater than [syncableInterval] to skip sync
		SyncMode:           block.StateSyncSkipped,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, vmsync.ParentsToFetch, testSetup)

	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func StateSyncFromScratchTest(t *testing.T, testSetup *SyncTestSetup) {
	rand.Seed(1)
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, vmsync.ParentsToFetch, testSetup)

	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func StateSyncFromScratchExceedParentTest(t *testing.T, testSetup *SyncTestSetup) {
	rand.Seed(1)
	numToGen := vmsync.ParentsToFetch + uint64(32)
	test := SyncTestParams{
		SyncableInterval:   numToGen,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, int(numToGen), testSetup)

	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func StateSyncToggleEnabledToDisabledTest(t *testing.T, testSetup *SyncTestSetup) {
	rand.Seed(1)
	// Hack: registering metrics uses global variables, so we need to disable metrics here so that we can initialize the VM twice.
	metrics.Enabled = false
	defer func() {
		metrics.Enabled = true
	}()

	var lock sync.Mutex
	reqCount := 0
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
		responseIntercept: func(syncerVM extension.InnerVM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Fail all requests after number 50 to interrupt the sync
			if reqCount > 50 {
				if err := syncerVM.AppRequestFailed(context.Background(), nodeID, requestID, commonEng.ErrTimeout); err != nil {
					panic(err)
				}
				if err := syncerVM.SyncerClient().Shutdown(); err != nil {
					panic(err)
				}
			} else {
				syncerVM.AppResponse(context.Background(), nodeID, requestID, response)
			}
		},
		expectedErr: context.Canceled,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, vmsync.ParentsToFetch, testSetup)

	// Perform sync resulting in early termination.
	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)

	test.SyncMode = block.StateSyncStatic
	test.responseIntercept = nil
	test.expectedErr = nil

	syncDisabledVM, _ := testSetup.NewVM()
	appSender := &enginetest.Sender{T: t}
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	appSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go testSyncVMSetup.serverVM.VM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}
	stateSyncDisabledConfigJSON := `{"state-sync-enabled":false}`
	if err := syncDisabledVM.Initialize(
		context.Background(),
		testSyncVMSetup.syncerVM.SnowCtx,
		testSyncVMSetup.syncerVM.DB,
		[]byte(GenesisJSONLatest),
		nil,
		[]byte(stateSyncDisabledConfigJSON),
		testSyncVMSetup.syncerVM.EngineChan,
		[]*commonEng.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := syncDisabledVM.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	if height := syncDisabledVM.LastAcceptedVMBlock().Height(); height != 0 {
		t.Fatalf("Unexpected last accepted height: %d", height)
	}

	enabled, err := syncDisabledVM.StateSyncEnabled(context.Background())
	assert.NoError(t, err)
	assert.False(t, enabled, "sync should be disabled")

	// Process the first 10 blocks from the serverVM
	for i := uint64(1); i < 10; i++ {
		ethBlock := testSyncVMSetup.serverVM.VM.Ethereum().BlockChain().GetBlockByNumber(i)
		if ethBlock == nil {
			t.Fatalf("VM Server did not have a block available at height %d", i)
		}
		b, err := rlp.EncodeToBytes(ethBlock)
		if err != nil {
			t.Fatal(err)
		}
		blk, err := syncDisabledVM.ParseBlock(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		if err := blk.Verify(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := blk.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// Verify the snapshot disk layer matches the last block root
	lastRoot := syncDisabledVM.Ethereum().BlockChain().CurrentBlock().Root
	if err := syncDisabledVM.Ethereum().BlockChain().Snapshots().Verify(lastRoot); err != nil {
		t.Fatal(err)
	}
	syncDisabledVM.Ethereum().BlockChain().DrainAcceptorQueue()

	// Create a new VM from the same database with state sync enabled.
	syncReEnabledVM, _ := testSetup.NewVM()
	// Enable state sync in configJSON
	configJSON := fmt.Sprintf(
		`{"state-sync-enabled":true, "state-sync-min-blocks":%d}`,
		test.StateSyncMinBlocks,
	)
	if err := syncReEnabledVM.Initialize(
		context.Background(),
		testSyncVMSetup.syncerVM.SnowCtx,
		testSyncVMSetup.syncerVM.DB,
		[]byte(GenesisJSONLatest),
		nil,
		[]byte(configJSON),
		testSyncVMSetup.syncerVM.EngineChan,
		[]*commonEng.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	testSyncVMSetup.serverVM.AppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncReEnabledVM.AppResponse(ctx, nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncReEnabledVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	assert.NoError(t, syncReEnabledVM.Connected(
		context.Background(),
		testSyncVMSetup.serverVM.SnowCtx.NodeID,
		statesyncclient.StateSyncVersion,
	))

	enabled, err = syncReEnabledVM.StateSyncEnabled(context.Background())
	assert.NoError(t, err)
	assert.True(t, enabled, "sync should be enabled")

	testSyncVMSetup.syncerVM.VM = syncReEnabledVM
	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func VMShutdownWhileSyncingTest(t *testing.T, testSetup *SyncTestSetup) {
	var (
		lock            sync.Mutex
		testSyncVMSetup *testSyncVMSetup
	)
	reqCount := 0
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
		responseIntercept: func(syncerVM extension.InnerVM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Shutdown the VM after 50 requests to interrupt the sync
			if reqCount == 50 {
				// Note this verifies the VM shutdown does not time out while syncing.
				require.NoError(t, testSyncVMSetup.syncerVM.shutdownOnceSyncerVM.Shutdown(context.Background()))
			} else if reqCount < 50 {
				err := syncerVM.AppResponse(context.Background(), nodeID, requestID, response)
				require.NoError(t, err)
			}
		},
		expectedErr: context.Canceled,
	}
	testSyncVMSetup = initSyncServerAndClientVMs(t, test, vmsync.ParentsToFetch, testSetup)
	// Perform sync resulting in early termination.
	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

type SyncTestSetup struct {
	NewVM             func() (extension.InnerVM, dummy.ConsensusCallbacks) // should not be initialized
	AfterInit         func(t *testing.T, testParams SyncTestParams, vmSetup VMSetup, isServer bool)
	GenFn             func(i int, vm extension.InnerVM, gen *core.BlockGen)
	ExtraSyncerVMTest func(t *testing.T, syncerVM VMSetup)
}

func initSyncServerAndClientVMs(t *testing.T, test SyncTestParams, numBlocks int, testSetup *SyncTestSetup) *testSyncVMSetup {
	require := require.New(t)

	// override commitInterval so the call to trie creates a commit at the height [syncableInterval].
	// This is necessary to support fetching a state summary.
	config := fmt.Sprintf(`{"commit-interval": %d, "state-sync-commit-interval": %d}`, test.SyncableInterval, test.SyncableInterval)
	serverVM, cb := testSetup.NewVM()
	serverChan, serverDB, serverAtomicMem, serverAppSender, serverCtx := SetupVM(t, true, GenesisJSONLatest, config, "", serverVM)
	t.Cleanup(func() {
		log.Info("Shutting down server VM")
		require.NoError(serverVM.Shutdown(context.Background()))
	})
	serverVmSetup := VMSetup{
		VM:                 serverVM,
		AppSender:          serverAppSender,
		SnowCtx:            serverCtx,
		ConsensusCallbacks: cb,
		DB:                 serverDB,
		EngineChan:         serverChan,
		AtomicMemory:       serverAtomicMem,
	}
	var err error
	if testSetup.AfterInit != nil {
		testSetup.AfterInit(t, test, serverVmSetup, true)
	}
	generateAndAcceptBlocks(t, serverVM, numBlocks, testSetup.GenFn, nil, cb)

	// make some accounts
	root, accounts := statesync.FillAccountsWithOverlappingStorage(t, serverVM.Ethereum().BlockChain().TrieDB(), types.EmptyRootHash, 1000, 16)

	// patch serverVM's lastAcceptedBlock to have the new root
	// and update the vm's state so the trie with accounts will
	// be returned by StateSyncGetLastSummary
	lastAccepted := serverVM.Ethereum().BlockChain().LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.Ethereum().ChainDb())
	blockBytes, err := rlp.EncodeToBytes(patchedBlock)
	require.NoError(err)
	internalWrappedBlock, err := serverVM.ParseBlock(context.Background(), blockBytes)
	require.NoError(err)
	internalBlock, ok := internalWrappedBlock.(*chain.BlockWrapper)
	require.True(ok)
	require.NoError(serverVM.SetLastAcceptedBlock(internalBlock.Block))

	// initialise [syncerVM] with blank genesis state
	// we also override [syncerVM]'s commit interval so the atomic trie works correctly.
	stateSyncEnabledJSON := fmt.Sprintf(`{"state-sync-enabled":true, "state-sync-min-blocks": %d, "tx-lookup-limit": %d, "commit-interval": %d}`, test.StateSyncMinBlocks, 4, test.SyncableInterval)

	syncerVM, syncerCB := testSetup.NewVM()
	syncerEngineChan, syncerDB, syncerAtomicMemory, syncerAppSender, chainCtx := SetupVM(t, false, GenesisJSONLatest, stateSyncEnabledJSON, "", syncerVM)
	shutdownOnceSyncerVM := &shutdownOnceVM{InnerVM: syncerVM}
	t.Cleanup(func() {
		require.NoError(shutdownOnceSyncerVM.Shutdown(context.Background()))
	})
	syncerVmSetup := syncerVMSetup{
		VMSetup: VMSetup{
			VM:                 syncerVM,
			ConsensusCallbacks: syncerCB,
			SnowCtx:            chainCtx,
			DB:                 syncerDB,
			EngineChan:         syncerEngineChan,
			AtomicMemory:       syncerAtomicMemory,
		},
		shutdownOnceSyncerVM: shutdownOnceSyncerVM,
	}
	if testSetup.AfterInit != nil {
		testSetup.AfterInit(t, test, syncerVmSetup.VMSetup, false)
	}
	require.NoError(syncerVM.SetState(context.Background(), snow.StateSyncing))
	enabled, err := syncerVM.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

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
	require.NoError(
		syncerVM.Connected(
			context.Background(),
			serverCtx.NodeID,
			statesyncclient.StateSyncVersion,
		),
	)

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerAppSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		require.True(hasItem, "expected nodeSet to contain at least 1 nodeID")
		err := serverVM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request)
		require.NoError(err)
		return nil
	}

	return &testSyncVMSetup{
		serverVM: VMSetup{
			VM:        serverVM,
			AppSender: serverAppSender,
			SnowCtx:   serverCtx,
		},
		fundedAccounts: accounts,
		syncerVM:       syncerVmSetup,
	}
}

// testSyncVMSetup contains the required set up for a client VM to perform state sync
// off of a server VM.
type testSyncVMSetup struct {
	serverVM VMSetup
	syncerVM syncerVMSetup

	fundedAccounts map[*keystore.Key]*types.StateAccount
}

type VMSetup struct {
	VM                 extension.InnerVM
	SnowCtx            *snow.Context
	ConsensusCallbacks dummy.ConsensusCallbacks
	DB                 avalanchedatabase.Database
	EngineChan         chan commonEng.Message
	AtomicMemory       *avalancheatomic.Memory
	AppSender          *enginetest.Sender
}

type syncerVMSetup struct {
	VMSetup
	shutdownOnceSyncerVM *shutdownOnceVM
}

type shutdownOnceVM struct {
	extension.InnerVM
	shutdownOnce sync.Once
}

func (vm *shutdownOnceVM) Shutdown(ctx context.Context) error {
	var err error
	vm.shutdownOnce.Do(func() { err = vm.InnerVM.Shutdown(ctx) })
	return err
}

// SyncTestParams contains both the actual VMs as well as the parameters with the expected output.
type SyncTestParams struct {
	responseIntercept  func(vm extension.InnerVM, nodeID ids.NodeID, requestID uint32, response []byte)
	StateSyncMinBlocks uint64
	SyncableInterval   uint64
	SyncMode           block.StateSyncMode
	expectedErr        error
}

func testSyncerVM(t *testing.T, testSyncVMSetup *testSyncVMSetup, test SyncTestParams, extraSyncerVMTest func(t *testing.T, syncerVMSetup VMSetup)) {
	t.Helper()
	var (
		require          = require.New(t)
		serverVM         = testSyncVMSetup.serverVM.VM
		fundedAccounts   = testSyncVMSetup.fundedAccounts
		syncerVM         = testSyncVMSetup.syncerVM.VM
		syncerEngineChan = testSyncVMSetup.syncerVM.EngineChan
	)
	// get last summary and test related methods
	summary, err := serverVM.GetLastStateSummary(context.Background())
	require.NoError(err, "error getting state sync last summary")
	parsedSummary, err := syncerVM.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err, "error parsing state summary")
	retrievedSummary, err := serverVM.GetStateSummary(context.Background(), parsedSummary.Height())
	require.NoError(err, "error getting state sync summary at height")
	require.Equal(summary, retrievedSummary)

	syncMode, err := parsedSummary.Accept(context.Background())
	require.NoError(err, "error accepting state summary")
	require.Equal(test.SyncMode, syncMode)
	if syncMode == block.StateSyncSkipped {
		return
	}

	msg := <-syncerEngineChan
	require.Equal(commonEng.StateSyncDone, msg)

	// If the test is expected to error, assert the correct error is returned and finish the test.
	err = syncerVM.SyncerClient().Error()
	if test.expectedErr != nil {
		require.ErrorIs(err, test.expectedErr)
		// Note we re-open the database here to avoid a closed error when the test is for a shutdown VM.
		// TODO: this avoids circular dependencies but is not ideal.
		ethDBPrefix := []byte("ethdb")
		chaindb := database.WrapDatabase(prefixdb.NewNested(ethDBPrefix, testSyncVMSetup.syncerVM.DB))
		assertSyncPerformedHeights(t, chaindb, map[uint64]struct{}{})
		return
	}
	require.NoError(err, "state sync failed")

	// set [syncerVM] to bootstrapping and verify the last accepted block has been updated correctly
	// and that we can bootstrap and process some blocks.
	require.NoError(syncerVM.SetState(context.Background(), snow.Bootstrapping))
	require.Equal(serverVM.LastAcceptedVMBlock().Height(), syncerVM.LastAcceptedVMBlock().Height(), "block height mismatch between syncer and server")
	require.Equal(serverVM.LastAcceptedVMBlock().ID(), syncerVM.LastAcceptedVMBlock().ID(), "blockID mismatch between syncer and server")
	require.True(syncerVM.Ethereum().BlockChain().HasState(syncerVM.Ethereum().BlockChain().LastAcceptedBlock().Root()), "unavailable state for last accepted block")
	assertSyncPerformedHeights(t, syncerVM.Ethereum().ChainDb(), map[uint64]struct{}{retrievedSummary.Height(): {}})

	lastNumber := syncerVM.Ethereum().BlockChain().LastAcceptedBlock().NumberU64()
	// check the last block is indexed
	lastSyncedBlock := rawdb.ReadBlock(syncerVM.Ethereum().ChainDb(), rawdb.ReadCanonicalHash(syncerVM.Ethereum().ChainDb(), lastNumber), lastNumber)
	require.NotNil(lastSyncedBlock, "last synced block not found")
	for _, tx := range lastSyncedBlock.Transactions() {
		index := rawdb.ReadTxLookupEntry(syncerVM.Ethereum().ChainDb(), tx.Hash())
		require.NotNilf(index, "Miss transaction indices, number %d hash %s", lastNumber, tx.Hash().Hex())
	}

	// tail should be the last block synced
	if syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory != 0 {
		tail := lastSyncedBlock.NumberU64()

		core.CheckTxIndices(t, &tail, tail, syncerVM.Ethereum().ChainDb(), true)
	}

	blocksToBuild := 10
	txsPerBlock := 10
	toAddress := TestEthAddrs[1] // arbitrary choice
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, vm extension.InnerVM, gen *core.BlockGen) {
		b, err := predicate.NewResults().Bytes()
		if err != nil {
			t.Fatal(err)
		}
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, InitialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), k.PrivateKey)
			require.NoError(err)
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	},
		func(block *types.Block) {
			if syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory != 0 {
				tail := block.NumberU64() - syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory + 1
				// tail should be the minimum last synced block, since we skipped it to the last block
				if tail < lastSyncedBlock.NumberU64() {
					tail = lastSyncedBlock.NumberU64()
				}
				core.CheckTxIndices(t, &tail, block.NumberU64(), syncerVM.Ethereum().ChainDb(), true)
			}
		},
		testSyncVMSetup.syncerVM.ConsensusCallbacks,
	)

	// check we can transition to [NormalOp] state and continue to process blocks.
	require.NoError(syncerVM.SetState(context.Background(), snow.NormalOp))

	// Generate blocks after we have entered normal consensus as well
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, vm extension.InnerVM, gen *core.BlockGen) {
		b, err := predicate.NewResults().Bytes()
		if err != nil {
			t.Fatal(err)
		}
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, InitialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), k.PrivateKey)
			require.NoError(err)
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	},
		func(block *types.Block) {
			if syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory != 0 {
				tail := block.NumberU64() - syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory + 1
				// tail should be the minimum last synced block, since we skipped it to the last block
				if tail < lastSyncedBlock.NumberU64() {
					tail = lastSyncedBlock.NumberU64()
				}
				core.CheckTxIndices(t, &tail, block.NumberU64(), syncerVM.Ethereum().ChainDb(), true)
			}
		},
		testSyncVMSetup.syncerVM.ConsensusCallbacks,
	)

	if extraSyncerVMTest != nil {
		extraSyncerVMTest(t, testSyncVMSetup.syncerVM.VMSetup)
	}
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
	newBlk := types.NewBlockWithExtData(
		header, blk.Transactions(), blk.Uncles(), receipts, trie.NewStackTrie(nil), blk.ExtData(), true,
	)
	rawdb.WriteBlock(db, newBlk)
	rawdb.WriteCanonicalHash(db, newBlk.Hash(), newBlk.NumberU64())
	return newBlk
}

// generateAndAcceptBlocks uses [core.GenerateChain] to generate blocks, then
// calls Verify and Accept on each generated block
// TODO: consider using this helper function in vm_test.go and elsewhere in this package to clean up tests
func generateAndAcceptBlocks(t *testing.T, vm extension.InnerVM, numBlocks int, gen func(int, extension.InnerVM, *core.BlockGen), accepted func(*types.Block), cb dummy.ConsensusCallbacks) {
	t.Helper()

	// acceptExternalBlock defines a function to parse, verify, and accept a block once it has been
	// generated by GenerateChain
	acceptExternalBlock := func(block *types.Block) {
		bytes, err := rlp.EncodeToBytes(block)
		if err != nil {
			t.Fatal(err)
		}
		vmBlock, err := vm.ParseBlock(context.Background(), bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Verify(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}

		if accepted != nil {
			accepted(block)
		}
	}
	_, _, err := core.GenerateChain(
		vm.Ethereum().BlockChain().Config(),
		vm.Ethereum().BlockChain().LastAcceptedBlock(),
		dummy.NewFakerWithCallbacks(cb),
		vm.Ethereum().ChainDb(),
		numBlocks,
		10,
		func(i int, g *core.BlockGen) {
			g.SetOnBlockGenerated(acceptExternalBlock)
			g.SetCoinbase(constants.BlackholeAddr) // necessary for syntactic validation of the block
			gen(i, vm, g)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.Ethereum().BlockChain().DrainAcceptorQueue()
}

// assertSyncPerformedHeights iterates over all heights the VM has synced to and
// verifies it matches [expected].
func assertSyncPerformedHeights(t *testing.T, db ethdb.Iteratee, expected map[uint64]struct{}) {
	it := rawdb.NewSyncPerformedIterator(db)
	defer it.Release()

	found := make(map[uint64]struct{}, len(expected))
	for it.Next() {
		found[rawdb.UnpackSyncPerformedKey(it.Key())] = struct{}{}
	}
	require.NoError(t, it.Error())
	require.Equal(t, expected, found)
}
