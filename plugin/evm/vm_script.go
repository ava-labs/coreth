// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

func (vm *VM) script(cfg *ethconfig.Config) error {
	log.Warn("VM SCRIPT: running")
	// Let's use a consistent recent state root for this test
	targetHeight := uint64(36077568)

	bc := vm.blockChain
	targetBlock := bc.GetBlockByNumber(targetHeight)
	if targetBlock == nil {
		return fmt.Errorf("cannot find target block with height: %d", targetHeight)
	}

	for i := 0; i < 100; i++ {
		if err := vm.measureOnce(cfg, targetBlock.Root(), i); err != nil {
			return err
		}
	}

	log.Warn("VM SCRIPT: complete")
	return errors.New("script complete, intentionally preventing further initialization")
}

func (vm *VM) measureOnce(cfg *ethconfig.Config, stateRoot common.Hash, iteration int) error {
	db := vm.chaindb
	targetAddr := common.HexToAddress("0xc0c5aa69dbe4d6dddfbc89c0957686ec60f24389")
	numKeysToUpdate := 500

	// Use a fresh triedb for each measurement
	triedb := trie.NewDatabaseWithConfig(db, &trie.Config{
		Cache:   cfg.TrieCleanCache,
		Journal: cfg.TrieCleanJournal,
	})
	database := state.NewDatabaseWithNodeDB(db, triedb)

	// Add some keys to the state
	totalTime := time.Duration(0)
	numSamples := 100
	for i := 0; i < numSamples; i++ {
		sampleID := iteration*numSamples + i
		start := time.Now()
		var err error
		stateRoot, err = addKeys(database, stateRoot, iteration*numSamples+i, numKeysToUpdate, targetAddr)
		if err != nil {
			return err
		}
		sampleTime := time.Since(start)
		log.Info("VM SCRIPT: sample", "sample", sampleID, "time", sampleTime)
		totalTime += sampleTime
	}
	log.Info("VM SCRIPT: average", "average", totalTime/time.Duration(numSamples), "iteration", iteration)
	return nil
}

func addKeys(database state.Database, stateRoot common.Hash, iteration, n int, addr common.Address) (common.Hash, error) {
	// Note we intentionally don't use the snapshot
	state, err := state.New(stateRoot, database, nil)
	if err != nil {
		return common.Hash{}, err
	}

	for i := 0; i < n; i++ {
		// Just some deterministic key/value pairs
		k := crypto.Keccak256Hash([]byte(fmt.Sprintf("key-%d-%d", iteration, i)))
		v := crypto.Keccak256Hash([]byte(fmt.Sprintf("value-%d-%d", iteration, i)))

		state.SetState(addr, k, v)
	}
	newRoot, err := state.Commit(false, true)
	if err != nil {
		return common.Hash{}, err
	}
	if newRoot == (common.Hash{}) || newRoot == types.EmptyRootHash || newRoot == stateRoot {
		return common.Hash{}, errors.New("invalid state root")
	}
	database.TrieDB().Dereference(stateRoot)
	return newRoot, nil
}
