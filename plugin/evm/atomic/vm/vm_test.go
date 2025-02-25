package vm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/testutils"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/utils"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAtomicTestVM() *VM {
	return WrapVM(&evm.VM{})
}

func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	baseFee *big.Int, // fee to use post-AP3
	keys []*secp256k1.PrivateKey, // Keys to import the funds
) (*atomic.Tx, error) {
	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(chainID, kc.Addresses(), ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	return atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), chainID, to, baseFee, kc, atomicUTXOs)
}

func GenesisAtomicVM(t *testing.T,
	finishBootstrapping bool,
	genesisJSON string,
	configJSON string,
	upgradeJSON string,
) (
	chan commonEng.Message,
	*VM,
	database.Database,
	*avalancheatomic.Memory,
	*enginetest.Sender,
) {
	vm := WrapVM(&evm.VM{})
	ch, db, m, sender, _ := testutils.SetupVM(t, finishBootstrapping, genesisJSON, configJSON, upgradeJSON, vm)
	return ch, vm, db, m, sender
}

func addUTXO(sharedMemory *avalancheatomic.Memory, ctx *snow.Context, txID ids.ID, index uint32, assetID ids.ID, amount uint64, addr ids.ShortID) (*avax.UTXO, error) {
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: index,
		},
		Asset: avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		return nil, err
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			addr.Bytes(),
		},
	}}}}); err != nil {
		return nil, err
	}

	return utxo, nil
}

// GenesisVMWithUTXOs creates a GenesisVM and generates UTXOs in the X-Chain Shared Memory containing AVAX based on the [utxos] map
// Generates UTXOIDs by using a hash of the address in the [utxos] map such that the UTXOs will be generated deterministically.
// If [testutils.GenesisJSON] is empty, defaults to using [testutils.GenesisJSONLatest]
func GenesisVMWithUTXOs(t *testing.T, finishBootstrapping bool, genesisJSON string, configJSON string, upgradeJSON string, utxos map[ids.ShortID]uint64) (chan commonEng.Message, *VM, database.Database, *avalancheatomic.Memory, *enginetest.Sender) {
	issuer, vm, db, sharedMemory, sender := GenesisAtomicVM(t, finishBootstrapping, genesisJSON, configJSON, upgradeJSON)
	for addr, avaxAmount := range utxos {
		txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
		if err != nil {
			t.Fatalf("Failed to generate txID from addr: %s", err)
		}
		if _, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AVAXAssetID, avaxAmount, addr); err != nil {
			t.Fatalf("Failed to add UTXO to shared memory: %s", err)
		}
	}

	return issuer, vm, db, sharedMemory, sender
}

func TestImportMissingUTXOs(t *testing.T) {
	// make a VM with a shared memory that has an importable UTXO to build a block
	importAmount := uint64(50000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, testutils.GenesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		testutils.TestShortIDAddrs[0]: importAmount,
	})
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, testutils.TestKeys[0:1])
	require.NoError(t, err)
	err = vm.mempool.AddLocalTx(importTx)
	require.NoError(t, err)
	<-issuer
	blk, err := vm.BuildBlock(context.Background())
	require.NoError(t, err)

	// make another VM which is missing the UTXO in shared memory
	_, vm2, _, _, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase2, "", "")
	defer func() {
		err := vm2.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	vm2Blk, err := vm2.ParseBlock(context.Background(), blk.Bytes())
	require.NoError(t, err)
	err = vm2Blk.Verify(context.Background())
	require.ErrorIs(t, err, errMissingUTXOs)

	// This should not result in a bad block since the missing UTXO should
	// prevent InsertBlockManual from being called.
	badBlocks, _ := vm2.Ethereum().BlockChain().BadBlocks()
	require.Len(t, badBlocks, 0)
}

// Simple test to ensure we can issue an import transaction followed by an export transaction
// and they will be indexed correctly when accepted.
func TestIssueAtomicTxs(t *testing.T) {
	importAmount := uint64(50000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, testutils.GenesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		testutils.TestShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, testutils.TestKeys[0:1])
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
	vm.Ethereum().BlockChain().DrainAcceptorQueue()

	state, err := vm.Ethereum().BlockChain().State()
	if err != nil {
		t.Fatal(err)
	}

	exportTx, err := atomic.NewExportTx(vm.ctx, vm.currentRules(), state, vm.ctx.AVAXAssetID, importAmount-(2*ap0.AtomicTxFee), vm.ctx.XChainID, testutils.TestShortIDAddrs[0], testutils.InitialBaseFee, testutils.TestKeys[0:1])
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(exportTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk2.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	// Check that both atomic transactions were indexed as expected.
	indexedImportTx, status, height, err := vm.getAtomicTx(importTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, atomic.Accepted, status)
	assert.Equal(t, uint64(1), height, "expected height of indexed import tx to be 1")
	assert.Equal(t, indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

	indexedExportTx, status, height, err := vm.getAtomicTx(exportTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, atomic.Accepted, status)
	assert.Equal(t, uint64(2), height, "expected height of indexed export tx to be 2")
	assert.Equal(t, indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
}

func testConflictingImportTxs(t *testing.T, genesis string) {
	importAmount := uint64(10000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesis, "", "", map[ids.ShortID]uint64{
		testutils.TestShortIDAddrs[0]: importAmount,
		testutils.TestShortIDAddrs[1]: importAmount,
		testutils.TestShortIDAddrs[2]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTxs := make([]*atomic.Tx, 0, 3)
	conflictTxs := make([]*atomic.Tx, 0, 3)
	for i, key := range testutils.TestKeys {
		importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[i], testutils.InitialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)

		conflictAddr := testutils.TestEthAddrs[(i+1)%len(testutils.TestEthAddrs)]
		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictAddr, testutils.InitialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		conflictTxs = append(conflictTxs, conflictTx)
	}

	expectedParentBlkID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range importTxs[:2] {
		if err := vm.mempool.AddLocalTx(tx); err != nil {
			t.Fatal(err)
		}

		<-issuer

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		blk, err := vm.BuildBlock(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if err := blk.Verify(context.Background()); err != nil {
			t.Fatal(err)
		}

		if parentID := blk.Parent(); parentID != expectedParentBlkID {
			t.Fatalf("Expected parent to have blockID %s, but found %s", expectedParentBlkID, parentID)
		}

		expectedParentBlkID = blk.ID()
		if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
			t.Fatal(err)
		}
	}

	// Check that for each conflict tx (whose conflict is in the chain ancestry)
	// the VM returns an error when it attempts to issue the conflict into the mempool
	// and when it attempts to build a block with the conflict force added to the mempool.
	for i, tx := range conflictTxs[:2] {
		if err := vm.mempool.AddLocalTx(tx); err == nil {
			t.Fatal("Expected issueTx to fail due to conflicting transaction")
		}
		// Force issue transaction directly to the mempool
		if err := vm.mempool.ForceAddTx(tx); err != nil {
			t.Fatal(err)
		}
		<-issuer

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		_, err = vm.BuildBlock(context.Background())
		// The new block is verified in BuildBlock, so
		// BuildBlock should fail due to an attempt to
		// double spend an atomic UTXO.
		if err == nil {
			t.Fatalf("Block verification should have failed in BuildBlock %d due to double spending atomic UTXO", i)
		}
	}

	// Generate one more valid block so that we can copy the header to create an invalid block
	// with modified extra data. This new block will be invalid for more than one reason (invalid merkle root)
	// so we check to make sure that the expected error is returned from block verification.
	if err := vm.mempool.AddLocalTx(importTxs[2]); err != nil {
		t.Fatal(err)
	}
	<-issuer
	vm.clock.Set(vm.clock.Time().Add(2 * time.Second))

	validBlock, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := validBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	validEthBlock := validBlock.(*chain.BlockWrapper).Block.(extension.VMBlock).GetEthBlock()

	rules := vm.currentRules()
	var extraData []byte
	switch {
	case rules.IsApricotPhase5:
		extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{conflictTxs[1]})
	default:
		extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, conflictTxs[1])
	}
	if err != nil {
		t.Fatal(err)
	}

	conflictingAtomicTxBlock := types.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err := rlp.EncodeToBytes(conflictingAtomicTxBlock)
	if err != nil {
		t.Fatal(err)
	}

	parsedBlock, err := vm.ParseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); !errors.Is(err, errConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", errConflictingAtomicInputs, err)
	}

	if !rules.IsApricotPhase5 {
		return
	}

	extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTxs[2], conflictTxs[2]})
	if err != nil {
		t.Fatal(err)
	}

	header := types.CopyHeader(validEthBlock.Header())
	headerExtra := types.GetHeaderExtra(header)
	headerExtra.ExtDataGasUsed.Mul(common.Big2, headerExtra.ExtDataGasUsed)

	internalConflictBlock := types.NewBlockWithExtData(
		header,
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err = rlp.EncodeToBytes(internalConflictBlock)
	if err != nil {
		t.Fatal(err)
	}

	parsedBlock, err = vm.ParseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); !errors.Is(err, errConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", errConflictingAtomicInputs, err)
	}
}

func TestReissueAtomicTxHigherGasPrice(t *testing.T) {
	kc := secp256k1fx.NewKeychain(testutils.TestKeys...)

	for name, issueTxs := range map[string]func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, discarded []*atomic.Tx){
		"single UTXO override": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testutils.TestShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], new(big.Int).Mul(common.Big2, testutils.InitialBaseFee), kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(tx1); err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(tx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"one of two UTXOs overrides": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testutils.TestShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testutils.TestShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], new(big.Int).Mul(common.Big2, testutils.InitialBaseFee), kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(tx1); err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(tx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"hola": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testutils.TestShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testutils.TestShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}

			importTx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			importTx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], new(big.Int).Mul(big.NewInt(3), testutils.InitialBaseFee), kc, []*avax.UTXO{utxo2})
			if err != nil {
				t.Fatal(err)
			}

			reissuanceTx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], new(big.Int).Mul(big.NewInt(2), testutils.InitialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(importTx1); err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(importTx2); err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(reissuanceTx1); !errors.Is(err, txpool.ErrConflictingAtomicTx) {
				t.Fatalf("Expected to fail with err: %s, but found err: %s", txpool.ErrConflictingAtomicTx, err)
			}

			assert.True(t, vm.mempool.Has(importTx1.ID()))
			assert.True(t, vm.mempool.Has(importTx2.ID()))
			assert.False(t, vm.mempool.Has(reissuanceTx1.ID()))

			reissuanceTx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], new(big.Int).Mul(big.NewInt(4), testutils.InitialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(reissuanceTx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{reissuanceTx2}, []*atomic.Tx{importTx1, importTx2}
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase5, "", "")
			issuedTxs, evictedTxs := issueTxs(t, vm, sharedMemory)

			for i, tx := range issuedTxs {
				_, issued := vm.mempool.GetPendingTx(tx.ID())
				assert.True(t, issued, "expected issued tx at index %d to be issued", i)
			}

			for i, tx := range evictedTxs {
				_, discarded, _ := vm.mempool.GetTx(tx.ID())
				assert.True(t, discarded, "expected discarded tx at index %d to be discarded", i)
			}
		})
	}
}

func TestConflictingImportTxsAcrossBlocks(t *testing.T) {
	for name, genesis := range map[string]string{
		"apricotPhase1": testutils.GenesisJSONApricotPhase1,
		"apricotPhase2": testutils.GenesisJSONApricotPhase2,
		"apricotPhase3": testutils.GenesisJSONApricotPhase3,
		"apricotPhase4": testutils.GenesisJSONApricotPhase4,
		"apricotPhase5": testutils.GenesisJSONApricotPhase5,
	} {
		t.Run(name, func(t *testing.T) {
			testConflictingImportTxs(t, genesis)
		})
	}
}

func TestConflictingTransitiveAncestryWithGap(t *testing.T) {
	key, err := utils.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key0 := testutils.TestKeys[0]
	addr0 := key0.Address()

	key1 := testutils.TestKeys[1]
	addr1 := key1.Address()

	importAmount := uint64(1000000000)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, testutils.GenesisJSONApricotPhase0, "", "",
		map[ids.ShortID]uint64{
			addr0: importAmount,
			addr1: importAmount,
		})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx0A, err := vm.newImportTx(vm.ctx.XChainID, key.Address, testutils.InitialBaseFee, []*secp256k1.PrivateKey{key0})
	if err != nil {
		t.Fatal(err)
	}
	// Create a conflicting transaction
	importTx0B, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[2], testutils.InitialBaseFee, []*secp256k1.PrivateKey{key0})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx0A); err != nil {
		t.Fatalf("Failed to issue importTx0A: %s", err)
	}

	<-issuer

	blk0, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk0.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk0.ID()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk0.ID()) {
		t.Fatalf("Expected new block to match")
	}

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs := vm.Ethereum().TxPool().AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer

	blk1, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build blk1: %s", err)
	}

	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatalf("blk1 failed verification due to %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk1.ID()); err != nil {
		t.Fatal(err)
	}

	importTx1, err := vm.newImportTx(vm.ctx.XChainID, key.Address, testutils.InitialBaseFee, []*secp256k1.PrivateKey{key1})
	if err != nil {
		t.Fatalf("Failed to issue importTx1 due to: %s", err)
	}

	if err := vm.mempool.AddLocalTx(importTx1); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk2.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx0B); err == nil {
		t.Fatalf("Should not have been able to issue import tx with conflict")
	}
	// Force issue transaction directly into the mempool
	if err := vm.mempool.ForceAddTx(importTx0B); err != nil {
		t.Fatal(err)
	}
	<-issuer

	_, err = vm.BuildBlock(context.Background())
	if err == nil {
		t.Fatal("Shouldn't have been able to build an invalid block")
	}
}

func TestBonusBlocksTxs(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase0, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testutils.TestKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testutils.TestKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, []*secp256k1.PrivateKey{testutils.TestKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Make [blk] a bonus block.
	vm.atomicBackend.AddBonusBlock(blk.Height(), blk.ID())

	// Remove the UTXOs from shared memory, so that non-bonus blocks will fail verification
	if err := vm.ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.XChainID: {RemoveRequests: [][]byte{inputID[:]}}}); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
}

// Builds [blkA] with a virtuous import transaction and [blkB] with a separate import transaction
// that does not conflict. Accepts [blkB] and rejects [blkA], then asserts that the virtuous atomic
// transaction in [blkA] is correctly re-issued into the atomic transaction mempool.
func TestReissueAtomicTx(t *testing.T) {
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, testutils.GenesisJSONApricotPhase1, "", "", map[ids.ShortID]uint64{
		testutils.TestShortIDAddrs[0]: 10000000,
		testutils.TestShortIDAddrs[1]: 10000000,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	genesisBlkID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, []*secp256k1.PrivateKey{testutils.TestKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blkA, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blkA.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blkA.ID()); err != nil {
		t.Fatal(err)
	}

	// SetPreference to parent before rejecting (will rollback state to genesis
	// so that atomic transaction can be reissued, otherwise current block will
	// conflict with UTXO to be reissued)
	if err := vm.SetPreference(context.Background(), genesisBlkID); err != nil {
		t.Fatal(err)
	}

	// Rejecting [blkA] should cause [importTx] to be re-issued into the mempool.
	if err := blkA.Reject(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Sleep for a minimum of two seconds to ensure that [blkB] will have a different timestamp
	// than [blkA] so that the block will be unique. This is necessary since we have marked [blkA]
	// as Rejected.
	time.Sleep(2 * time.Second)
	<-issuer
	blkB, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if blkB.Height() != blkA.Height() {
		t.Fatalf("Expected blkB (%d) to have the same height as blkA (%d)", blkB.Height(), blkA.Height())
	}

	if err := blkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blkB.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blkB.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blkB.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blkB.ID(), lastAcceptedID)
	}

	// Check that [importTx] has been indexed correctly after [blkB] is accepted.
	_, height, err := vm.atomicTxRepository.GetByTxID(importTx.ID())
	if err != nil {
		t.Fatal(err)
	} else if height != blkB.Height() {
		t.Fatalf("Expected indexed height of import tx to be %d, but found %d", blkB.Height(), height)
	}
}

func TestAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase1, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	exportTxs := createExportTxOptions(t, vm, issuer, sharedMemory)
	exportTx1, exportTx2 := exportTxs[0], exportTxs[1]

	if err := vm.mempool.AddLocalTx(exportTx1); err != nil {
		t.Fatal(err)
	}
	<-issuer
	exportBlk1, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := exportBlk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), exportBlk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(exportTx2); err == nil {
		t.Fatal("Should have failed to issue due to an invalid export tx")
	}

	if err := vm.mempool.AddRemoteTx(exportTx2); err == nil {
		t.Fatal("Should have failed to add because conflicting")
	}

	// Manually add transaction to mempool to bypass validation
	if err := vm.mempool.ForceAddTx(exportTx2); err != nil {
		t.Fatal(err)
	}
	<-issuer

	_, err = vm.BuildBlock(context.Background())
	if err == nil {
		t.Fatal("BuildBlock should have returned an error due to invalid export transaction")
	}
}

// This is a regression test to ensure that if two consecutive atomic transactions fail verification
// in onFinalizeAndAssemble it will not cause a panic due to calling RevertToSnapshot(revID) on the
// same revision ID twice.
func TestConsecutiveAtomicTransactionsRevertSnapshot(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase1, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	// Create three conflicting import transactions
	importTxs := createImportTxOptions(t, vm, sharedMemory)

	// Issue the first import transaction, build, and accept the block.
	if err := vm.mempool.AddLocalTx(importTxs[0]); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Add the two conflicting transactions directly to the mempool, so that two consecutive transactions
	// will fail verification when build block is called.
	vm.mempool.AddRemoteTx(importTxs[1])
	vm.mempool.AddRemoteTx(importTxs[2])

	if _, err := vm.BuildBlock(context.Background()); err == nil {
		t.Fatal("Expected build block to fail due to empty block")
	}
}

func TestAtomicTxBuildBlockDropsConflicts(t *testing.T) {
	importAmount := uint64(10000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, testutils.GenesisJSONApricotPhase5, "", "", map[ids.ShortID]uint64{
		testutils.TestShortIDAddrs[0]: importAmount,
		testutils.TestShortIDAddrs[1]: importAmount,
		testutils.TestShortIDAddrs[2]: importAmount,
	})
	conflictKey, err := utils.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a conflict set for each pair of transactions
	conflictSets := make([]set.Set[ids.ID], len(testutils.TestKeys))
	for index, key := range testutils.TestKeys {
		importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[index], testutils.InitialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.mempool.AddLocalTx(importTx); err != nil {
			t.Fatal(err)
		}
		conflictSets[index].Add(importTx.ID())
		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictKey.Address, testutils.InitialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.mempool.AddLocalTx(conflictTx); err == nil {
			t.Fatal("should conflict with the utxoSet in the mempool")
		}
		// force add the tx
		vm.mempool.ForceAddTx(conflictTx)
		conflictSets[index].Add(conflictTx.ID())
	}
	<-issuer
	// Note: this only checks the path through OnFinalizeAndAssemble, we should make sure to add a test
	// that verifies blocks received from the network will also fail verification
	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	atomicTxs, err := extractAtomicTxsFromBlock(wrappedBlk, vm.Ethereum().BlockChain().Config())
	require.NoError(t, err)
	assert.True(t, len(atomicTxs) == len(testutils.TestKeys), "Conflict transactions should be out of the batch")
	atomicTxIDs := set.Set[ids.ID]{}
	for _, tx := range atomicTxs {
		atomicTxIDs.Add(tx.ID())
	}

	// Check that removing the txIDs actually included in the block from each conflict set
	// leaves one item remaining for each conflict set ie. only one tx from each conflict set
	// has been included in the block.
	for _, conflictSet := range conflictSets {
		conflictSet.Difference(atomicTxIDs)
		assert.Equal(t, 1, conflictSet.Len())
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBlockDoesNotExceedAtomicGasLimit(t *testing.T) {
	importAmount := uint64(10000000)
	issuer, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testutils.TestKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testutils.TestShortIDAddrs[0][:]))
	assert.NoError(t, err)

	mempoolTxs := 200
	for i := 0; i < mempoolTxs; i++ {
		utxo, err := addUTXO(sharedMemory, vm.ctx, txID, uint32(i), vm.ctx.AVAXAssetID, importAmount, testutils.TestShortIDAddrs[0])
		assert.NoError(t, err)

		importTx, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, kc, []*avax.UTXO{utxo})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.mempool.AddLocalTx(importTx); err != nil {
			t.Fatal(err)
		}
	}

	<-issuer
	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	atomicTxs, err := extractAtomicTxsFromBlock(wrappedBlk, vm.Ethereum().BlockChain().Config())
	require.NoError(t, err)
	// Need to ensure that not all of the transactions in the mempool are included in the block.
	// This ensures that we hit the atomic gas limit while building the block before we hit the
	// upper limit on the size of the codec for marshalling the atomic transactions.
	if len(atomicTxs) >= mempoolTxs {
		t.Fatalf("Expected number of atomic transactions included in the block (%d) to be less than the number of transactions added to the mempool (%d)", len(atomicTxs), mempoolTxs)
	}
}

func TestExtraStateChangeAtomicGasLimitExceeded(t *testing.T) {
	importAmount := uint64(10000000)
	// We create two VMs one in ApriotPhase4 and one in ApricotPhase5, so that we can construct a block
	// containing a large enough atomic transaction that it will exceed the atomic gas limit in
	// ApricotPhase5.
	issuer, vm1, _, sharedMemory1, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase4, "", "")
	_, vm2, _, sharedMemory2, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testutils.TestKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testutils.TestShortIDAddrs[0][:]))
	assert.NoError(t, err)

	// Add enough UTXOs, such that the created import transaction will attempt to consume more gas than allowed
	// in ApricotPhase5.
	for i := 0; i < 100; i++ {
		_, err := addUTXO(sharedMemory1, vm1.ctx, txID, uint32(i), vm1.ctx.AVAXAssetID, importAmount, testutils.TestShortIDAddrs[0])
		assert.NoError(t, err)

		_, err = addUTXO(sharedMemory2, vm2.ctx, txID, uint32(i), vm2.ctx.AVAXAssetID, importAmount, testutils.TestShortIDAddrs[0])
		assert.NoError(t, err)
	}

	// Double the initial base fee used when estimating the cost of this transaction to ensure that when it is
	// used in ApricotPhase5 it still pays a sufficient fee with the fixed fee per atomic transaction.
	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, testutils.TestEthAddrs[0], new(big.Int).Mul(common.Big2, testutils.InitialBaseFee), []*secp256k1.PrivateKey{testutils.TestKeys[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm1.mempool.ForceAddTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer
	blk1, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	wrappedBlk, ok := blk1.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	validEthBlock := wrappedBlk.GetEthBlock()
	extraData, err := atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTx})
	if err != nil {
		t.Fatal(err)
	}

	// Construct the new block with the extra data in the new format (slice of atomic transactions).
	ethBlk2 := types.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	state, err := vm2.Ethereum().BlockChain().State()
	if err != nil {
		t.Fatal(err)
	}

	// Hack: test [onExtraStateChange] directly to ensure it catches the atomic gas limit error correctly.
	if _, _, err := vm2.onExtraStateChange(ethBlk2, state, vm2.Ethereum().BlockChain().Config()); err == nil || !strings.Contains(err.Error(), "exceeds atomic gas limit") {
		t.Fatalf("Expected block to fail verification due to exceeded atomic gas limit, but found error: %v", err)
	}
}

// Regression test to ensure that a VM that is not able to parse a block that
// contains no transactions.
func TestEmptyBlock(t *testing.T) {
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, testutils.GenesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testutils.TestShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testutils.TestEthAddrs[0], testutils.InitialBaseFee, []*secp256k1.PrivateKey{testutils.TestKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	ethBlock := wrappedBlk.GetEthBlock()

	emptyEthBlock := types.NewBlockWithExtData(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		nil,
		false,
	)

	if len(emptyEthBlock.ExtData()) != 0 || types.GetHeaderExtra(emptyEthBlock.Header()).ExtDataHash != (common.Hash{}) {
		t.Fatalf("emptyEthBlock should not have any extra data")
	}

	emptyBlock, err := vm.NewVMBlock(emptyEthBlock)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := vm.ParseBlock(context.Background(), emptyBlock.Bytes()); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("VM should have failed with errEmptyBlock but got %s", err.Error())
	}
	if err := emptyBlock.Verify(context.Background()); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("block should have failed verification with errEmptyBlock but got %s", err.Error())
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 5 ruleset in genesis.
func TestBuildApricotPhase5Block(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testutils.TestKeys[0].ToECDSA()
	address := testutils.TestEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testutils.TestKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testutils.TestKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, testutils.InitialBaseFee, []*secp256k1.PrivateKey{testutils.TestKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	ethBlk := wrappedBlk.GetEthBlock()
	if eBlockGasCost := ethBlk.BlockGasCost(); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := ethBlk.ExtDataGasUsed(); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(11230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 11230 but got %d", eExtDataGasUsed)
	}
	extraConfig := params.GetExtra(vm.Ethereum().BlockChain().Config())
	minRequiredTip, err := header.EstimateRequiredTip(extraConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(common.Big0) != 0 {
		t.Fatalf("expected minRequiredTip to be 0 but got %d", minRequiredTip)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice*3), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.Ethereum().TxPool().Add(txs, false, false)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err = vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	wrappedBlk, ok = blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	ethBlk = wrappedBlk.GetEthBlock()
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	if ethBlk.ExtDataGasUsed() == nil || ethBlk.ExtDataGasUsed().Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", ethBlk.ExtDataGasUsed())
	}
	minRequiredTip, err = header.EstimateRequiredTip(extraConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*params.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
	}

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := vm.Ethereum().BlockChain().GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 4 ruleset in genesis.
func TestBuildApricotPhase4Block(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase4, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testutils.TestKeys[0].ToECDSA()
	address := testutils.TestEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testutils.TestKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testutils.TestKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, testutils.InitialBaseFee, []*secp256k1.PrivateKey{testutils.TestKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	ethBlk := wrappedBlk.GetEthBlock()
	if eBlockGasCost := ethBlk.BlockGasCost(); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := ethBlk.ExtDataGasUsed(); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(1230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 1000 but got %d", eExtDataGasUsed)
	}
	extraConfig := params.GetExtra(vm.Ethereum().BlockChain().Config())
	minRequiredTip, err := header.EstimateRequiredTip(extraConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(common.Big0) != 0 {
		t.Fatalf("expected minRequiredTip to be 0 but got %d", minRequiredTip)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.Ethereum().TxPool().AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err = vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	wrappedBlk, ok = blk.(*chain.BlockWrapper).Block.(extension.VMBlock)
	require.True(t, ok, "expected block to be a VMBlock")
	ethBlk = wrappedBlk.GetEthBlock()
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	if ethBlk.ExtDataGasUsed() == nil || ethBlk.ExtDataGasUsed().Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", ethBlk.ExtDataGasUsed())
	}
	minRequiredTip, err = header.EstimateRequiredTip(extraConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*params.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
	}

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := vm.Ethereum().BlockChain().GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}
}

func TestBuildInvalidBlockHead(t *testing.T) {
	issuer, vm, _, _, _ := GenesisAtomicVM(t, true, testutils.GenesisJSONApricotPhase0, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	key0 := testutils.TestKeys[0]
	addr0 := key0.Address()

	// Create the transaction
	utx := &atomic.UnsignedImportTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		Outs: []atomic.EVMOutput{{
			Address: common.Address(addr0),
			Amount:  1 * units.Avax,
			AssetID: vm.ctx.AVAXAssetID,
		}},
		ImportedInputs: []*avax.TransferableInput{
			{
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1 * units.Avax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		SourceChain: vm.ctx.XChainID,
	}
	tx := &atomic.Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key0}}); err != nil {
		t.Fatal(err)
	}

	currentBlock := vm.Ethereum().BlockChain().CurrentBlock()

	// Verify that the transaction fails verification when attempting to issue
	// it into the atomic mempool.
	if err := vm.mempool.AddLocalTx(tx); err == nil {
		t.Fatal("Should have failed to issue invalid transaction")
	}
	// Force issue the transaction directly to the mempool
	if err := vm.mempool.ForceAddTx(tx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	if _, err := vm.BuildBlock(context.Background()); err == nil {
		t.Fatalf("Unexpectedly created a block")
	}

	newCurrentBlock := vm.Ethereum().BlockChain().CurrentBlock()

	if currentBlock.Hash() != newCurrentBlock.Hash() {
		t.Fatal("current block changed")
	}
}
