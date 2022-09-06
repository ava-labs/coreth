package evm

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestPrecompileConfigs(t *testing.T) {
	require := require.New(t)
	_, vmPre6, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhasePre6, "", "")
	require.Equal(vmPre6.chainConfig.ApricotPhasePre6BlockTimestamp.Cmp(common.Big0), 0)
	require.Nil(vmPre6.chainConfig.ApricotPhase6BlockTimestamp)
	require.Nil(vmPre6.chainConfig.ApricotPhasePost6BlockTimestamp)

	_, vm6, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhase6, "", "")
	require.Equal(vm6.chainConfig.ApricotPhasePre6BlockTimestamp.Cmp(common.Big0), 0)
	require.Equal(vm6.chainConfig.ApricotPhase6BlockTimestamp.Cmp(common.Big0), 0)
	require.Nil(vm6.chainConfig.ApricotPhasePost6BlockTimestamp)

	_, vmPost6, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhasePost6, "", "")
	require.Equal(vmPost6.chainConfig.ApricotPhasePre6BlockTimestamp.Cmp(common.Big0), 0)
	require.Equal(vmPost6.chainConfig.ApricotPhase6BlockTimestamp.Cmp(common.Big0), 0)
	require.Equal(vmPost6.chainConfig.ApricotPhasePost6BlockTimestamp.Cmp(common.Big0), 0)
}

func testPrecompileBlock(t *testing.T, config *params.ChainConfig, tx *types.Transaction, expectedErr error) {
	require := require.New(t)
	importAmount := uint64(20000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "{\"pruning-enabled\":true}", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk1, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk1.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk1.Accept(); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk1.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Add a transaction

	// assert the behavior of that transaction
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{tx})
	require.Len(errs, 1)
	require.Nil(errs[0])

	// Same behavior from miner as in verification, so this is all we need to check
	_, err = vm.miner.GenerateBlock()
	if err != nil {
		t.Fatal(err)
	}

	if expectedErr == nil {
		require.Nil(err)
		return
	}

	require.True(errors.Is(err, expectedErr))
	// We want to check the behavior of the tx pool to confirm that it's been removed.
}
