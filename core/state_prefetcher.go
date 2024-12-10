// Copyright 2019 The go-ethereum Authors
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
	"encoding/binary"
	"math/big"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
	"golang.org/x/sync/errgroup"
)

// statePrefetcher is a basic Prefetcher, which blindly executes a block on top
// of an arbitrary state with the goal of prefetching potentially useful state
// data from disk before the main block processor start executing.
type statePrefetcher struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// newStatePrefetcher initialises a new statePrefetcher.
func newStatePrefetcher(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *statePrefetcher {
	return &statePrefetcher{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Prefetch processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb, but any changes are discarded. The
// only goal is to pre-cache transaction signatures and state trie nodes.
type tape []byte

func (t tape) Len() int { return len(t) }

func (p *statePrefetcher) Prefetch(block *types.Block, parentRoot common.Hash, cfg vm.Config, tape *tape) {
	if p.bc.snaps == nil {
		log.Warn("Skipping prefetching transactions without snapshot cache")
		return
	}
	snap := p.bc.snaps.Snapshot(parentRoot)
	if snap == nil {
		log.Warn("Skipping prefetching transactions without snapshot cache")
		return
	}

	var (
		header       = block.Header()
		gaspool      = new(GasPool).AddGas(block.GasLimit())
		blockContext = NewEVMBlockContext(header, p.bc, nil)
		signer       = types.MakeSigner(p.config, header.Number, header.Time)
	)
	statedb, err := state.New(parentRoot, p.bc.stateCache, p.bc.snaps)
	if err != nil {
		return
	}
	var eg errgroup.Group
	eg.SetLimit(1) // Some limits just in case.
	// Iterate over and process the individual transactions
	vmState := &StateReadsRecorder{vmStateDB: statedb}
	for i, tx := range block.Transactions() {
		evm := vm.NewEVM(blockContext, vm.TxContext{}, vmState, p.config, cfg)
		eg.Go(func() error {
			// Convert the transaction into an executable message and pre-cache its sender
			msg, err := TransactionToMessage(tx, signer, header.BaseFee)
			if err != nil {
				return err // Also invalid block, bail out
			}
			vmState.SetTxContext(tx.Hash(), i)
			if err := precacheTransaction(msg, p.config, gaspool, vmState, header, evm); err != nil {
				// NOTE: We don't care that the the transaction failed, we just want to pre-cache
				return err // Ugh, something went horribly wrong, bail out
			}
			return nil
		})
		// If we're pre-byzantium, pre-load trie nodes for the intermediate root
		// if !byzantium {
		// 	statedb.IntermediateRoot(true)
		// }
	}
	// NOTE: For now I don't want to deal with trie nodes, just snapshot cache.
	// If were post-byzantium, pre-load trie nodes for the final root hash
	// if byzantium {
	// 	statedb.IntermediateRoot(true)
	// }

	// Wait for all transactions to be processed
	if err := eg.Wait(); err != nil {
		log.Error("Unexpected failure in pre-caching transactions", "err", err)
	}
	*tape = vmState.tape
}

// precacheTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. The goal is not to execute
// the transaction successfully, rather to warm up touched data slots.
func precacheTransaction(msg *Message, config *params.ChainConfig, gaspool *GasPool, statedb vm.StateDB, header *types.Header, evm *vm.EVM) error {
	// Update the evm with the new transaction context.
	evm.Reset(NewEVMTxContext(msg), statedb)
	// Add addresses to access list if applicable
	_, err := ApplyMessage(evm, msg, gaspool)
	return err
}

type vmStateDB interface {
	vm.StateDB
	Finalise(bool)
	IntermediateRoot(bool) common.Hash
	SetTxContext(common.Hash, int)
	TxIndex() int
	GetLogs(common.Hash, uint64, common.Hash) []*types.Log
}

type StateReadsRecorder struct {
	vmStateDB

	tape []byte
}

type StateReadsReplayer struct {
	vmStateDB

	tape []byte
}

func (s *StateReadsRecorder) GetBalance(addr common.Address) *uint256.Int {
	v := s.vmStateDB.GetBalance(addr)
	bytes := v.Bytes()
	s.tape = append(s.tape, byte(len(bytes)))
	s.tape = append(s.tape, bytes...)
	return v
}

func (s *StateReadsReplayer) GetBalance(common.Address) *uint256.Int {
	l := int(s.tape[0])
	v := new(uint256.Int)
	v.SetBytes(s.tape[1 : 1+l])
	s.tape = s.tape[1+l:]
	return v
}

func (s *StateReadsRecorder) GetBalanceMultiCoin(addr common.Address, coin common.Hash) *big.Int {
	v := s.vmStateDB.GetBalanceMultiCoin(addr, coin)
	bytes := v.Bytes()
	s.tape = append(s.tape, byte(len(bytes)))
	s.tape = append(s.tape, v.Bytes()...)
	return v
}

func (s *StateReadsReplayer) GetBalanceMultiCoin(common.Address, common.Hash) *big.Int {
	l := int(s.tape[0])
	v := new(big.Int)
	v.SetBytes(s.tape[1 : 1+l])
	s.tape = s.tape[1+l:]
	return v
}

func (s *StateReadsRecorder) GetNonce(addr common.Address) uint64 {
	v := s.vmStateDB.GetNonce(addr)
	s.tape = binary.BigEndian.AppendUint64(s.tape, v)
	return v
}

func (s *StateReadsReplayer) GetNonce(common.Address) uint64 {
	v := binary.BigEndian.Uint64(s.tape)
	s.tape = s.tape[8:]
	return v
}

func (s *StateReadsRecorder) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	v := s.vmStateDB.GetCommittedState(addr, hash)
	s.tape = append(s.tape, v.Bytes()...)
	return v
}

func (s *StateReadsReplayer) GetCommittedState(common.Address, common.Hash) common.Hash {
	v := common.BytesToHash(s.tape[:32])
	s.tape = s.tape[32:]
	return v
}

func (s *StateReadsRecorder) GetCommittedStateAP1(addr common.Address, hash common.Hash) common.Hash {
	v := s.vmStateDB.GetCommittedStateAP1(addr, hash)
	s.tape = append(s.tape, v.Bytes()...)
	return v
}

func (s *StateReadsReplayer) GetCommittedStateAP1(common.Address, common.Hash) common.Hash {
	v := common.BytesToHash(s.tape[:32])
	s.tape = s.tape[32:]
	return v
}

func (s *StateReadsRecorder) GetState(addr common.Address, hash common.Hash) common.Hash {
	v := s.vmStateDB.GetState(addr, hash)
	s.tape = append(s.tape, v.Bytes()...)
	return v
}

func (s *StateReadsReplayer) GetState(common.Address, common.Hash) common.Hash {
	v := common.BytesToHash(s.tape[:32])
	s.tape = s.tape[32:]
	return v
}
