// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ethereum/go-ethereum/log"
)

const (
	repairDone     = math.MaxUint64         // used as a marker for when the height map is repaired
	iterationDelay = 100 * time.Microsecond // delay between iterations of the repair loop
)

func (a *atomicTrie) RepairHeightMap(to uint64, iterationDelay time.Duration) (bool, error) {
	repairFrom, err := database.GetUInt64(a.metadataDB, heightMapRepairKey)
	switch {
	case errors.Is(err, database.ErrNotFound):
		repairFrom = 0 // height map not repaired yet, start at 0
	case err != nil:
		return false, err
	case repairFrom == repairDone:
		// height map already repaired, nothing to do
		return false, nil
	}
	return true, a.repairHeightMap(repairFrom, to, iterationDelay)
}

func (a *atomicTrie) repairHeightMap(from, to uint64, iterationDelay time.Duration) error {
	// open the atomic trie at the last known root with correct height map
	// correspondance
	fromRoot, err := getRoot(a.metadataDB, from)
	if err != nil {
		return fmt.Errorf("could not get root at height %d: %w", from, err)
	}
	hasher, err := a.OpenTrie(fromRoot)
	if err != nil {
		return fmt.Errorf("could not open atomic trie at root %s: %w", fromRoot, err)
	}

	// hashes values inserted in [hasher], and stores the result in the height
	// map at [commitHeight]. Additionally, it updates the resume marker and
	// re-opens [hasher] to respect the trie's no use after commit invariant.
	lastLog := from
	logEach := uint64(100_000)
	commitRepairedHeight := func(commitHeight uint64) error {
		root, nodes := hasher.Commit(false)
		if nodes != nil {
			err := a.trieDB.Update(root, types.EmptyRootHash, trienode.NewWithNodeSet(nodes))
			if err != nil {
				return err
			}
		}
		err = a.trieDB.Commit(root, false)
		if err != nil {
			return err
		}
		err = a.metadataDB.Put(database.PackUInt64(commitHeight), root[:])
		if err != nil {
			return err
		}
		err = database.PutUInt64(a.metadataDB, heightMapRepairKey, commitHeight)
		if err != nil {
			return err
		}
		if commitHeight >= lastLog+logEach {
			log.Info("repaired atomic trie height map", "height", commitHeight, "root", root)
			lastLog = commitHeight
		}
		hasher, err = a.OpenTrie(root)
		return err
	}

	// iterate over all leaves in the current atomic trie
	root, _ := a.LastCommitted()
	it, err := a.Iterator(root, database.PackUInt64(from+1))
	if err != nil {
		return fmt.Errorf("could not create iterator for atomic trie at root %s: %w", root, err)
	}

	var height uint64
	lastCommit := from
	for it.Next() {
		height = it.BlockNumber()
		if height > to {
			break
		}

		for next := lastCommit + a.commitInterval; next < height; next += a.commitInterval {
			if err := commitRepairedHeight(next); err != nil {
				return err
			}
			lastCommit = next
		}

		if err := hasher.Update(it.Key(), it.Value()); err != nil {
			return fmt.Errorf("could not update atomic trie at root %s: %w", root, err)
		}

		time.Sleep(iterationDelay) // pause to avoid putting a spike of load on the disk
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("error iterating atomic trie: %w", err)
	}
	for next := lastCommit + a.commitInterval; next <= to; next += a.commitInterval {
		if err := commitRepairedHeight(next); err != nil {
			return err
		}
	}

	// mark height map as repaired
	return database.PutUInt64(a.metadataDB, heightMapRepairKey, repairDone)
}
