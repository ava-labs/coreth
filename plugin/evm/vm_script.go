// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func (vm *VM) script() error {
	log.Warn("VM SCRIPT: running")

	vm.blockChain.SetAccessRecordingPrefixes(
		rawdb.FirstAccessSnapshotAccountPrefix,
		rawdb.FirstAccessSnapshotStoragePrefix,
	)
	if err := vm.reprocess(vm.config.ReprocessTo); err != nil {
		return fmt.Errorf("while running reprocessing: %w", err)
	}
	log.Warn("VM SCRIPT: First pass complete")
	vm.blockChain.SetAccessRecordingPrefixes(nil, nil)

	if err := rawdb.ApplyPrefix(
		vm.chaindb,
		rawdb.SnapshotAccountPrefix,
		rawdb.FirstAccessSnapshotAccountPrefix,
		len(rawdb.FirstAccessSnapshotAccountPrefix)+common.HashLength,
	); err != nil {
		return err
	}
	if err := rawdb.ApplyPrefix(
		vm.chaindb,
		rawdb.SnapshotStoragePrefix,
		rawdb.FirstAccessSnapshotStoragePrefix,
		len(rawdb.FirstAccessSnapshotStoragePrefix)+2*common.HashLength,
	); err != nil {
		return err
	}

	if err := rawdb.ClearPrefix(
		vm.chaindb,
		rawdb.FirstAccessSnapshotAccountPrefix,
		len(rawdb.FirstAccessSnapshotAccountPrefix)+common.HashLength,
	); err != nil {
		return err
	}
	if err := rawdb.ClearPrefix(
		vm.chaindb,
		rawdb.FirstAccessSnapshotStoragePrefix,
		len(rawdb.FirstAccessSnapshotStoragePrefix)+2*common.HashLength,
	); err != nil {
		return err
	}
	log.Warn("VM SCRIPT: Snapshot slots updated")

	// Set the last accepted to the first block to reprocess and go again
	// This also initializes the snapshots and updates the on-disk snapshot
	// pointers.
	firstBlock := vm.blockChain.GetBlockByNumber(vm.config.ReprocessFrom)
	if err := vm.blockChain.ResetToStateSyncedBlock(firstBlock); err != nil {
		return err
	}
	log.Warn("VM SCRIPT: Chain state reset")
	if err := vm.reprocess(vm.config.ReprocessTo); err != nil {
		return fmt.Errorf("while running reprocessing with snapshot: %w", err)
	}
	log.Warn("VM SCRIPT: complete")
	return errors.New("intentionally stopping VM from initializing, snapshot is not valid")
}

func (vm *VM) reprocess(origin uint64) error {
	var (
		bc           = vm.blockChain
		start        = time.Now()
		logged       = time.Now()
		previousRoot common.Hash
		triedb       = bc.TrieDB()
		current      = bc.LastAcceptedBlock()
	)
	// Note: we add 1 since in each iteration, we attempt to re-execute the next block.
	log.Info("Re-executing blocks to generate state for last accepted block", "from", current.NumberU64()+1, "to", origin)
	for current.NumberU64() < origin {
		// Print progress logs each block
		log.Info("Regenerating historical state",
			"block", current.NumberU64()+1,
			"target", origin,
			"remaining", origin-current.NumberU64(),
			"elapsed", time.Since(start),
			"last", time.Since(logged),
		)
		logged = time.Now()

		// Retrieve the next block to regenerate and process it
		parent := current
		next := current.NumberU64() + 1
		if current = bc.GetBlockByNumber(next); current == nil {
			return fmt.Errorf("failed to retrieve block %d while re-generating state", next)
		}

		// Reprocess next block using previously fetched data
		root, err := bc.ReprocessBlock(parent, current)
		if err != nil {
			return err
		}

		// Flatten snapshot if initialized, holding a reference to the state root until the next block
		// is processed.
		if err := bc.FlattenSnapshot(func() error {
			triedb.Reference(root, common.Hash{})
			if previousRoot != (common.Hash{}) {
				triedb.Dereference(previousRoot)
			}
			previousRoot = root

			// Commit the trieDB if we reach a CommitInterval
			if next%vm.config.CommitInterval == 0 {
				if err := triedb.Commit(previousRoot, true); err != nil {
					return err
				}
			}
			return nil
		}, current.Hash()); err != nil {
			return err
		}
	}

	nodes, imgs := triedb.Size()
	log.Info("Historical state regenerated", "block", current.NumberU64(), "elapsed", time.Since(start), "nodes", nodes, "preimages", imgs)
	if previousRoot != (common.Hash{}) {
		return triedb.Commit(previousRoot, true)
	}
	return nil
}
