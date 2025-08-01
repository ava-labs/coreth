// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/misc/eip4844"
	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
)

// BlockGen creates blocks for testing.
// See GenerateChain for a detailed explanation.
type BlockGen struct {
	i       int
	cm      *chainMaker
	parent  *types.Block
	header  *types.Header
	statedb *state.StateDB

	gasPool  *GasPool
	txs      []*types.Transaction
	receipts []*types.Receipt
	uncles   []*types.Header

	engine           consensus.Engine
	onBlockGenerated func(*types.Block)
}

// SetCoinbase sets the coinbase of the generated block.
// It can be called at most once.
func (b *BlockGen) SetCoinbase(addr common.Address) {
	if b.gasPool != nil {
		if len(b.txs) > 0 {
			panic("coinbase must be set before adding transactions")
		}
		panic("coinbase can only be set once")
	}
	b.header.Coinbase = addr
	b.gasPool = new(GasPool).AddGas(b.header.GasLimit)
}

// SetExtra sets the extra data field of the generated block.
func (b *BlockGen) SetExtra(data []byte) {
	b.header.Extra = data
}

// AppendExtra appends data to the extra data field of the generated block.
func (b *BlockGen) AppendExtra(data []byte) {
	b.header.Extra = append(b.header.Extra, data...)
}

// SetNonce sets the nonce field of the generated block.
func (b *BlockGen) SetNonce(nonce types.BlockNonce) {
	b.header.Nonce = nonce
}

// SetDifficulty sets the difficulty field of the generated block. This method is
// useful for Clique tests where the difficulty does not depend on time. For the
// ethash tests, please use OffsetTime, which implicitly recalculates the diff.
func (b *BlockGen) SetDifficulty(diff *big.Int) {
	b.header.Difficulty = diff
}

// Difficulty returns the currently calculated difficulty of the block.
func (b *BlockGen) Difficulty() *big.Int {
	return new(big.Int).Set(b.header.Difficulty)
}

// SetParentBeaconRoot sets the parent beacon root field of the generated
// block.
func (b *BlockGen) SetParentBeaconRoot(root common.Hash) {
	b.header.ParentBeaconRoot = &root
	var (
		blockContext = NewEVMBlockContext(b.header, b.cm, &b.header.Coinbase)
		vmenv        = vm.NewEVM(blockContext, vm.TxContext{}, b.statedb, b.cm.config, vm.Config{})
	)
	ProcessBeaconBlockRoot(root, vmenv, b.statedb)
}

// addTx adds a transaction to the generated block. If no coinbase has
// been set, the block's coinbase is set to the zero address.
//
// There are a few options can be passed as well in order to run some
// customized rules.
// - bc:       enables the ability to query historical block hashes for BLOCKHASH
// - vmConfig: extends the flexibility for customizing evm rules, e.g. enable extra EIPs
func (b *BlockGen) addTx(bc *BlockChain, vmConfig vm.Config, tx *types.Transaction) {
	if b.gasPool == nil {
		b.SetCoinbase(common.Address{})
	}
	b.statedb.SetTxContext(tx.Hash(), len(b.txs))
	blockContext := NewEVMBlockContext(b.header, bc, &b.header.Coinbase)
	receipt, err := ApplyTransaction(b.cm.config, bc, blockContext, b.gasPool, b.statedb, b.header, tx, &b.header.GasUsed, vmConfig)
	if err != nil {
		panic(err)
	}
	b.txs = append(b.txs, tx)
	b.receipts = append(b.receipts, receipt)
	if b.header.BlobGasUsed != nil {
		*b.header.BlobGasUsed += receipt.BlobGasUsed
	}
}

// AddTx adds a transaction to the generated block. If no coinbase has
// been set, the block's coinbase is set to the zero address.
//
// AddTx panics if the transaction cannot be executed. In addition to the protocol-imposed
// limitations (gas limit, etc.), there are some further limitations on the content of
// transactions that can be added. Notably, contract code relying on the BLOCKHASH
// instruction will panic during execution if it attempts to access a block number outside
// of the range created by GenerateChain.
func (b *BlockGen) AddTx(tx *types.Transaction) {
	b.addTx(nil, vm.Config{}, tx)
}

// AddTxWithChain adds a transaction to the generated block. If no coinbase has
// been set, the block's coinbase is set to the zero address.
//
// AddTxWithChain panics if the transaction cannot be executed. In addition to the
// protocol-imposed limitations (gas limit, etc.), there are some further limitations on
// the content of transactions that can be added. If contract code relies on the BLOCKHASH
// instruction, the block in chain will be returned.
func (b *BlockGen) AddTxWithChain(bc *BlockChain, tx *types.Transaction) {
	b.addTx(bc, vm.Config{}, tx)
}

// AddTxWithVMConfig adds a transaction to the generated block. If no coinbase has
// been set, the block's coinbase is set to the zero address.
// The evm interpreter can be customized with the provided vm config.
func (b *BlockGen) AddTxWithVMConfig(tx *types.Transaction, config vm.Config) {
	b.addTx(nil, config, tx)
}

// GetBalance returns the balance of the given address at the generated block.
func (b *BlockGen) GetBalance(addr common.Address) *uint256.Int {
	return b.statedb.GetBalance(addr)
}

// AddUncheckedTx forcefully adds a transaction to the block without any validation.
//
// AddUncheckedTx will cause consensus failures when used during real
// chain processing. This is best used in conjunction with raw block insertion.
func (b *BlockGen) AddUncheckedTx(tx *types.Transaction) {
	b.txs = append(b.txs, tx)
}

// Number returns the block number of the block being generated.
func (b *BlockGen) Number() *big.Int {
	return new(big.Int).Set(b.header.Number)
}

// Timestamp returns the timestamp of the block being generated.
func (b *BlockGen) Timestamp() uint64 {
	return b.header.Time
}

// BaseFee returns the EIP-1559 base fee of the block being generated.
func (b *BlockGen) BaseFee() *big.Int {
	return new(big.Int).Set(b.header.BaseFee)
}

// Gas returns the amount of gas left in the current block.
func (b *BlockGen) Gas() uint64 {
	return b.header.GasLimit - b.header.GasUsed
}

// Signer returns a valid signer instance for the current block.
func (b *BlockGen) Signer() types.Signer {
	return types.MakeSigner(b.cm.config, b.header.Number, b.header.Time)
}

// AddUncheckedReceipt forcefully adds a receipts to the block without a
// backing transaction.
//
// AddUncheckedReceipt will cause consensus failures when used during real
// chain processing. This is best used in conjunction with raw block insertion.
func (b *BlockGen) AddUncheckedReceipt(receipt *types.Receipt) {
	b.receipts = append(b.receipts, receipt)
}

// TxNonce returns the next valid transaction nonce for the
// account at addr. It panics if the account does not exist.
func (b *BlockGen) TxNonce(addr common.Address) uint64 {
	if !b.statedb.Exist(addr) {
		panic("account does not exist")
	}
	return b.statedb.GetNonce(addr)
}

// AddUncle adds an uncle header to the generated block.
func (b *BlockGen) AddUncle(h *types.Header) {
	b.uncles = append(b.uncles, h)
}

// PrevBlock returns a previously generated block by number. It panics if
// num is greater or equal to the number of the block being generated.
// For index -1, PrevBlock returns the parent block given to GenerateChain.
func (b *BlockGen) PrevBlock(index int) *types.Block {
	if index >= b.i {
		panic(fmt.Errorf("block index %d out of range (%d,%d)", index, -1, b.i))
	}
	if index == -1 {
		return b.cm.bottom
	}
	return b.cm.chain[index]
}

// OffsetTime modifies the time instance of a block, implicitly changing its
// associated difficulty. It's useful to test scenarios where forking is not
// tied to chain length directly.
func (b *BlockGen) OffsetTime(seconds int64) {
	b.header.Time += uint64(seconds)
	if b.header.Time <= b.cm.bottom.Header().Time {
		panic("block time out of range")
	}
	b.header.Difficulty = b.engine.CalcDifficulty(b.cm, b.header.Time, b.parent.Header())
}

// SetOnBlockGenerated sets a callback function to be invoked after each block is generated
func (b *BlockGen) SetOnBlockGenerated(onBlockGenerated func(*types.Block)) {
	b.onBlockGenerated = onBlockGenerated
}

// GenerateChain creates a chain of n blocks. The first block's
// parent will be the provided parent. db is used to store
// intermediate states and should contain the parent's state trie.
//
// The generator function is called with a new block generator for
// every block. Any transactions and uncles added to the generator
// become part of the block. If gen is nil, the blocks will be empty
// and their coinbase will be the zero address.
//
// Blocks created by GenerateChain do not contain valid proof of work
// values. Inserting them into BlockChain requires use of FakePow or
// a similar non-validating proof of work implementation.
func GenerateChain(config *params.ChainConfig, parent *types.Block, engine consensus.Engine, db ethdb.Database, n int, gap uint64, gen func(int, *BlockGen)) ([]*types.Block, []types.Receipts, error) {
	if config == nil {
		config = params.TestChainConfig
	}
	if engine == nil {
		panic("nil consensus engine")
	}
	cm := newChainMaker(parent, config, engine)

	genblock := func(i int, parent *types.Block, triedb *triedb.Database, statedb *state.StateDB) (*types.Block, types.Receipts, error) {
		b := &BlockGen{i: i, cm: cm, parent: parent, statedb: statedb, engine: engine}
		b.header = cm.makeHeader(parent, gap, statedb, b.engine)

		err := ApplyUpgrades(config, &parent.Header().Time, b, statedb)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to configure precompiles %w", err)
		}

		// Execute any user modifications to the block
		if gen != nil {
			gen(i, b)
		}
		// Finalize and seal the block
		block, err := b.engine.FinalizeAndAssemble(cm, b.header, parent.Header(), statedb, b.txs, b.uncles, b.receipts)
		if err != nil {
			return nil, nil, fmt.Errorf("Failed to finalize and assemble block at index %d: %w", i, err)
		}

		// Write state changes to db
		root, err := statedb.Commit(b.header.Number.Uint64(), config.IsEIP158(b.header.Number))
		if err != nil {
			panic(fmt.Sprintf("state write error: %v", err))
		}
		if err = triedb.Commit(root, false); err != nil {
			panic(fmt.Sprintf("trie write error: %v", err))
		}
		if b.onBlockGenerated != nil {
			b.onBlockGenerated(block)
		}
		return block, b.receipts, nil
	}

	// Forcibly use hash-based state scheme for retaining all nodes in disk.
	triedb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer triedb.Close()

	for i := 0; i < n; i++ {
		statedb, err := state.New(parent.Root(), extstate.NewDatabaseWithNodeDB(db, triedb), nil)
		if err != nil {
			return nil, nil, err
		}
		block, receipts, err := genblock(i, parent, triedb, statedb)
		if err != nil {
			return nil, nil, err
		}

		// Post-process the receipts.
		// Here we assign the final block hash and other info into the receipt.
		// In order for DeriveFields to work, the transaction and receipt lists need to be
		// of equal length. If AddUncheckedTx or AddUncheckedReceipt are used, there will be
		// extra ones, so we just trim the lists here.
		receiptsCount := len(receipts)
		txs := block.Transactions()
		if len(receipts) > len(txs) {
			receipts = receipts[:len(txs)]
		} else if len(receipts) < len(txs) {
			txs = txs[:len(receipts)]
		}
		var blobGasPrice *big.Int
		if block.ExcessBlobGas() != nil {
			blobGasPrice = eip4844.CalcBlobFee(*block.ExcessBlobGas())
		}
		if err := receipts.DeriveFields(config, block.Hash(), block.NumberU64(), block.Time(), block.BaseFee(), blobGasPrice, txs); err != nil {
			panic(err)
		}

		// Re-expand to ensure all receipts are returned.
		receipts = receipts[:receiptsCount]

		// Advance the chain.
		cm.add(block, receipts)
		parent = block
	}
	return cm.chain, cm.receipts, nil
}

// GenerateChainWithGenesis is a wrapper of GenerateChain which will initialize
// genesis block to database first according to the provided genesis specification
// then generate chain on top.
func GenerateChainWithGenesis(genesis *Genesis, engine consensus.Engine, n int, gap uint64, gen func(int, *BlockGen)) (ethdb.Database, []*types.Block, []types.Receipts, error) {
	db := rawdb.NewMemoryDatabase()
	triedb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer triedb.Close()
	_, err := genesis.Commit(db, triedb)
	if err != nil {
		return nil, nil, nil, err
	}
	blocks, receipts, err := GenerateChain(genesis.Config, genesis.ToBlock(), engine, db, n, gap, gen)
	return db, blocks, receipts, err
}

func (cm *chainMaker) makeHeader(parent *types.Block, gap uint64, state *state.StateDB, engine consensus.Engine) *types.Header {
	time := parent.Time() + gap // block time is fixed at [gap] seconds

	config := params.GetExtra(cm.config)
	gasLimit, err := header.GasLimit(config, parent.Header(), time)
	if err != nil {
		panic(err)
	}
	baseFee, err := header.BaseFee(config, parent.Header(), time)
	if err != nil {
		panic(err)
	}

	header := &types.Header{
		Root:       state.IntermediateRoot(cm.config.IsEIP158(parent.Number())),
		ParentHash: parent.Hash(),
		Coinbase:   parent.Coinbase(),
		Difficulty: engine.CalcDifficulty(cm, time, parent.Header()),
		GasLimit:   gasLimit,
		Number:     new(big.Int).Add(parent.Number(), common.Big1),
		Time:       time,
		BaseFee:    baseFee,
	}

	if cm.config.IsCancun(header.Number, header.Time) {
		var (
			parentExcessBlobGas uint64
			parentBlobGasUsed   uint64
		)
		if parent.ExcessBlobGas() != nil {
			parentExcessBlobGas = *parent.ExcessBlobGas()
			parentBlobGasUsed = *parent.BlobGasUsed()
		}
		excessBlobGas := eip4844.CalcExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed)
		header.ExcessBlobGas = &excessBlobGas
		header.BlobGasUsed = new(uint64)
		header.ParentBeaconRoot = new(common.Hash)
	}
	return header
}

// chainMaker contains the state of chain generation.
type chainMaker struct {
	bottom      *types.Block
	engine      consensus.Engine
	config      *params.ChainConfig
	chain       []*types.Block
	chainByHash map[common.Hash]*types.Block
	receipts    []types.Receipts
}

func newChainMaker(bottom *types.Block, config *params.ChainConfig, engine consensus.Engine) *chainMaker {
	return &chainMaker{
		bottom:      bottom,
		config:      config,
		engine:      engine,
		chainByHash: make(map[common.Hash]*types.Block),
	}
}

func (cm *chainMaker) add(b *types.Block, r []*types.Receipt) {
	cm.chain = append(cm.chain, b)
	cm.chainByHash[b.Hash()] = b
	cm.receipts = append(cm.receipts, r)
}

func (cm *chainMaker) blockByNumber(number uint64) *types.Block {
	if number == cm.bottom.NumberU64() {
		return cm.bottom
	}
	cur := cm.CurrentHeader().Number.Uint64()
	lowest := cm.bottom.NumberU64() + 1
	if number < lowest || number > cur {
		return nil
	}
	return cm.chain[number-lowest]
}

// ChainReader/ChainContext implementation

// Config returns the chain configuration (for consensus.ChainReader).
func (cm *chainMaker) Config() *params.ChainConfig {
	return cm.config
}

// Engine returns the consensus engine (for ChainContext).
func (cm *chainMaker) Engine() consensus.Engine {
	return cm.engine
}

func (cm *chainMaker) CurrentHeader() *types.Header {
	if len(cm.chain) == 0 {
		return cm.bottom.Header()
	}
	return cm.chain[len(cm.chain)-1].Header()
}

func (cm *chainMaker) GetHeaderByNumber(number uint64) *types.Header {
	b := cm.blockByNumber(number)
	if b == nil {
		return nil
	}
	return b.Header()
}

func (cm *chainMaker) GetHeaderByHash(hash common.Hash) *types.Header {
	b := cm.chainByHash[hash]
	if b == nil {
		return nil
	}
	return b.Header()
}

func (cm *chainMaker) GetHeader(hash common.Hash, number uint64) *types.Header {
	return cm.GetHeaderByNumber(number)
}

func (cm *chainMaker) GetBlock(hash common.Hash, number uint64) *types.Block {
	return cm.blockByNumber(number)
}
