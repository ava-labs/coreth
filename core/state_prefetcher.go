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
	"sync/atomic"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
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
func (p *statePrefetcher) Prefetch(block *types.Block, parentRoot common.Hash, cfg vm.Config, interrupt *atomic.Bool) {
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
	for i, tx := range block.Transactions() {
		// If block precaching was interrupted, abort
		if interrupt != nil && interrupt.Load() {
			return
		}
		eg.Go(func() error {
			evm := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
			// Convert the transaction into an executable message and pre-cache its sender
			msg, err := TransactionToMessage(tx, signer, header.BaseFee)
			if err != nil {
				return err // Also invalid block, bail out
			}
			statedb.SetTxContext(tx.Hash(), i)
			if err := precacheTransaction(msg, p.config, gaspool, statedb, header, evm); err != nil {
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
		log.Crit("Unexpected failure in pre-caching transactions", "err", err)
	}
}

// precacheTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. The goal is not to execute
// the transaction successfully, rather to warm up touched data slots.
func precacheTransaction(msg *Message, config *params.ChainConfig, gaspool *GasPool, statedb *state.StateDB, header *types.Header, evm *vm.EVM) error {
	// Update the evm with the new transaction context.
	evm.Reset(NewEVMTxContext(msg), statedb)
	// Add addresses to access list if applicable
	_, err := ApplyMessage(evm, msg, gaspool)
	return err
}
