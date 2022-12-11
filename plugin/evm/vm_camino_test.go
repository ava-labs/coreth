// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/assert"
)

var (
	sunriseBaseFee              = big.NewInt(int64(params.SunrisePhase0BaseFee))
	apricotPhase5BlockTimestamp = big.NewInt(time.Now().Add(time.Minute * -20).Unix())
	sunrisePhase0BlockTimestamp = big.NewInt(time.Now().Add(time.Minute * -10).Unix())

	timestamps = "\"apricotPhase1BlockTimestamp\": 0," +
		"\"apricotPhase2BlockTimestamp\": 0," +
		"\"apricotPhase3BlockTimestamp\": 0, " +
		"\"apricotPhase4BlockTimestamp\": 0, " +
		"\"apricotPhase5BlockTimestamp\": " + fmt.Sprint(apricotPhase5BlockTimestamp) + ", " +
		" \"sunrisePhase0BlockTimestamp\": " + fmt.Sprint(sunrisePhase0BlockTimestamp)

	genesisJSONSunrisePhase0WithCustomTimestamps = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0," + timestamps + "},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONSunrisePhase0                     = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0,\"apricotPhase3BlockTimestamp\":0,\"apricotPhase4BlockTimestamp\":0, \"apricotPhase5BlockTimestamp\":0,\"sunrisePhase0BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
)

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 5 ruleset AND Sunrise Phase 0 in genesis.
func TestSunrisePhase0AndApricotPhase5Block(t *testing.T) {
	importAmount := uint64(1_000_000_000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(
		t,
		true,
		genesisJSONSunrisePhase0WithCustomTimestamps,
		"{\"pruning-enabled\":true}",
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
			testShortIDAddrs[1]: importAmount,
		})

	defer func() {
		if err := vm.Shutdown(context.TODO()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)

	// set vm's time 1min after apricot phase 5
	// vm.clock.Set(apricotPhase5Timestamp.Add(time.Minute * 1))
	vm.clock.Set(time.Unix(apricotPhase5BlockTimestamp.Int64(), 0).Add(time.Minute * 1))

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[1], big.NewInt(0).Mul(initialBaseFee, big.NewInt(10)), []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	// Block A should be validated with apricot rules
	blkA, err := vm.BuildBlock(context.TODO())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}
	if err := blkA.Verify(context.TODO()); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}
	if status := blkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	// set vm's time 1min after sunrise phase 0
	// vm.clock.Set(apricotPhase5Timestamp.Add(time.Minute * 21))
	vm.clock.Set(time.Unix(sunrisePhase0BlockTimestamp.Int64(), 0).Add(time.Minute))

	importTx2, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[2], sunriseBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[1]})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.issueTx(importTx2, true /*=local*/); err != nil {
		t.Fatal(err)
	}
	<-issuer

	// Block B is the first block to be built after sunrise0 timestamp should be validated with apricot rules
	blkB, err := vm.BuildBlock(context.TODO())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}
	if err := blkB.Verify(context.TODO()); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}
	if status := blkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	importTx3, err := vm.newExportTx(vm.ctx.AVAXAssetID, uint64(50000), vm.ctx.XChainID, testShortIDAddrs[0], big.NewInt(0).Mul(sunriseBaseFee, big.NewInt(100)), []*crypto.PrivateKeySECP256K1R{testKeys[1]})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.issueTx(importTx3, true /*=local*/); err != nil {
		t.Fatal(err)
	}
	<-issuer

	// Block C is the second block to be built after sunrise0 timestamp should be validated with apricot rules
	blkC, err := vm.BuildBlock(context.TODO())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}
	if err := blkC.Verify(context.TODO()); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}
	if status := blkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}
	// assertions
	blkAEthBlockA := blkA.(*chain.BlockWrapper).Block.(*Block).ethBlock
	blkAEthBlockB := blkB.(*chain.BlockWrapper).Block.(*Block).ethBlock
	blkAEthBlockC := blkC.(*chain.BlockWrapper).Block.(*Block).ethBlock
	assert.Equal(t, initialBaseFee, blkAEthBlockA.Header().BaseFee) // that's alright, see dynamic_fees.go#CalcBaseFee
	assert.Equal(t, params.ApricotPhase4MinBaseFee, blkAEthBlockB.Header().BaseFee.Int64())
	assert.Equal(t, sunriseBaseFee, blkAEthBlockC.Header().BaseFee)
}

// Regression test to ensure a normally created Sunrise Phase 0 Block A will be validated,
// a Sunrise Phase 0 Block B with empty body and Block A as a parent will not be validated and
// a Sunrise Phase 0 Block C with a missing parent will be syntactically validated (but not verified due to missing parent)
// TODO temporarily disabled. Useful for testing further phase progressions
func DisabledTestSunrisePhase0OrphanBlock(t *testing.T) {
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONSunrisePhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.TODO()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	address := testEthAddrs[0]
	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, sunriseBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	// Block A should be validated normally
	blkA, err := vm.BuildBlock(context.TODO())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blkA.Verify(context.TODO()); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}

	if status := blkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(context.TODO(), blkA.ID()); err != nil {
		t.Fatal(err)
	}

	blkAEthBlock := blkA.(*chain.BlockWrapper).Block.(*Block).ethBlock

	// Assertion for BaseFee
	assert.EqualValues(t, blkAEthBlock.BaseFee(), sunriseBaseFee, "Block's Base fee should be SunrisePhase0's Base Fee")

	extraData, err := vm.codec.Marshal(codecVersion, []*Tx{importTx})
	if err != nil {
		t.Fatal(err)
	}

	// Manually created Eth Block with a manually created header
	emptyEthBlk := types.NewBlock(
		&types.Header{
			Coinbase:    common.Address{1},
			ParentHash:  blkAEthBlock.Hash(),
			UncleHash:   types.EmptyUncleHash,
			TxHash:      types.EmptyRootHash,
			ReceiptHash: types.EmptyRootHash,
			Difficulty:  math.BigPow(1, 1),
			Number:      math.BigPow(2, 9),
			GasLimit:    params.ApricotPhase1GasLimit,
			GasUsed:     0,
			Time:        9876543,
			BaseFee:     initialBaseFee,
			ExtDataHash: common.HexToHash("28def515f5d63c6e29f0ea8abd5f5674333e76393530560e2740dcdd66298f86"),
		},
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		false,
	)

	emptyBlock := &Block{
		vm:       vm,
		ethBlock: emptyEthBlk,
		id:       ids.ID(emptyEthBlk.Hash()),
	}

	// Block B is empty created but with a valid parent therefore, its verification should return an error
	assert.ErrorIs(t, emptyBlock.Verify(context.TODO()), errEmptyBlock)

	// Manually created Eth Block with a manually created header
	orphanEthBlock := types.NewBlock(
		&types.Header{
			ParentHash:  common.Hash{},
			Difficulty:  math.BigPow(11, 11),
			Number:      math.BigPow(2, 9),
			GasLimit:    12345678,
			GasUsed:     1476322,
			Time:        9876543,
			ExtDataHash: common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		},
		nil,
		nil,
		nil,
		new(trie.Trie),
		nil,
		false,
	)
	// Manually created block with a dummy parent
	orphanBlock := &Block{
		vm:       vm,
		ethBlock: orphanEthBlock,
		id:       ids.ID(orphanEthBlock.Hash()),
	}

	// orphanBlock does not have a valid parent therefore, it shouldn't return an error from Syntactic Verification.
	// It should return a "rejected parent" error though, from Atomic TXs Verification where it's being ensured
	// that the parent was verified and inserted correctly.
	err = orphanBlock.Verify(context.TODO())
	assert.ErrorIs(t, err, errRejectedParent)
	checks := vm.DeferedChecks.deferedChecks
	assert.Equal(t, checks[ids.ID(common.Hash{})], orphanBlock)
}
