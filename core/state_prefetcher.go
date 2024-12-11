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
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
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
	recorder := &snapRecorder{Snapshot: snap}
	statedb, err := state.NewWithSnapshot(parentRoot, p.bc.stateCache, recorder)
	if err != nil {
		return
	}

	// Configure any upgrades that should go into effect during this block.
	parent := p.bc.GetHeader(block.ParentHash(), block.NumberU64()-1)
	err = ApplyUpgrades(p.config, &parent.Time, block, statedb)
	if err != nil {
		log.Error("failed to configure precompiles processing block", "hash", block.Hash(), "number", block.NumberU64(), "timestamp", block.Time(), "err", err)
	}

	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		vmenv := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
		ProcessBeaconBlockRoot(*beaconRoot, vmenv, statedb)
	}
	var eg errgroup.Group
	eg.SetLimit(1) // Some limits just in case.
	// Iterate over and process the individual transactions
	results := make([]*ExecutionResult, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		evm := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
		eg.Go(func() error {
			// Convert the transaction into an executable message and pre-cache its sender
			msg, err := TransactionToMessage(tx, signer, header.BaseFee)
			if err != nil {
				return err // Also invalid block, bail out
			}
			statedb.SetTxContext(tx.Hash(), i)
			if results[i], err = precacheTransaction(msg, p.config, gaspool, statedb, header, evm); err != nil {
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

	// hack: just setting the gas used for now
	receipts := make(types.Receipts, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		receipts[i] = &types.Receipt{
			TxHash: tx.Hash(),
		}
		if results[i] != nil {
			receipts[i].GasUsed = results[i].UsedGas
		}
	}
	if err := p.engine.Finalize(p.bc, block, parent, statedb, receipts); err != nil {
		log.Error("Failed to finalize block", "err", err)
	}

	*tape = recorder.tape
}

// precacheTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. The goal is not to execute
// the transaction successfully, rather to warm up touched data slots.
func precacheTransaction(msg *Message, config *params.ChainConfig, gaspool *GasPool, statedb vm.StateDB, header *types.Header, evm *vm.EVM) (*ExecutionResult, error) {
	// Update the evm with the new transaction context.
	evm.Reset(NewEVMTxContext(msg), statedb)
	// Add addresses to access list if applicable
	er, err := ApplyMessage(evm, msg, gaspool)
	return er, err
}

type vmStateDB interface {
	vm.StateDB
	Finalise(bool)
	IntermediateRoot(bool) common.Hash
	SetTxContext(common.Hash, int)
	TxIndex() int
	GetLogs(common.Hash, uint64, common.Hash) []*types.Log
}

type snapRecorder struct {
	snapshot.Snapshot

	tape tape
}

type snapReplay struct {
	snapshot.Snapshot

	tape tape
}

func (s *snapRecorder) Account(accHash common.Hash) (*types.SlimAccount, error) {
	acc, err := s.Snapshot.Account(accHash)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		// fmt.Println("nil account added")
		s.tape = append(s.tape, 0)
		return nil, nil
	}

	rlp, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return nil, err
	}
	// fmt.Println("account added", len(rlp))
	s.tape = append(s.tape, byte(len(rlp)))
	s.tape = append(s.tape, rlp...)
	return acc, err
}

func (s *snapRecorder) Storage(accHash common.Hash, hash common.Hash) ([]byte, error) {
	val, err := s.Snapshot.Storage(accHash, hash)
	if err != nil {
		return nil, err
	}
	// fmt.Println("storage added", len(val))
	s.tape = append(s.tape, byte(len(val)))
	s.tape = append(s.tape, val...)
	return val, nil
}

func (s *snapReplay) Account(accHash common.Hash) (*types.SlimAccount, error) {
	length := int(s.tape[0])
	s.tape = s.tape[1:]
	if length == 0 {
		// fmt.Println("nil account replayed")
		return nil, nil
	}

	// fmt.Println("account replayed", length)
	acc := new(types.SlimAccount)
	if err := rlp.DecodeBytes(s.tape[:length], acc); err != nil {
		return nil, err
	}
	s.tape = s.tape[length:]
	return acc, nil
}

func (s *snapReplay) Storage(accHash common.Hash, hash common.Hash) ([]byte, error) {
	length := int(s.tape[0])
	s.tape = s.tape[1:]
	// fmt.Println("storage replayed", length)

	val := s.tape[:length]
	s.tape = s.tape[length:]
	return val, nil
}
