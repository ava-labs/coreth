// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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

	"github.com/ava-labs/coreth/core/state"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

const (
	maxTrieInterval uint64 = 1024
)

type TrieWriter interface {
	InsertTrie(root common.Hash) error // Insert reference to trie [root]
	AcceptTrie(root common.Hash) error // Mark [root] as part of an accepted block
	RejectTrie(root common.Hash) error // Notify TrieWriter that the block containing [root] has been rejected
	Shutdown() error
}

func NewTrieWriter(db state.Database, config *CacheConfig) TrieWriter {
	if config.TrieDirtyDisabled {
		return &noPruningTrieWriter{
			Database: db,
		}
	} else {
		return &cappedMemoryTrieWriter{
			Database:          db,
			memoryCap:         common.StorageSize(config.TrieDirtyLimit) * 1024 * 1024,
			imageCap:          4 * 1024 * 1024,
			maxBlocksAccepted: maxTrieInterval,
		}
	}
}

type noPruningTrieWriter struct {
	state.Database
}

func (np *noPruningTrieWriter) InsertTrie(root common.Hash) error {
	triedb := np.Database.TrieDB()
	return triedb.Commit(root, false, nil)
}

func (np *noPruningTrieWriter) AcceptTrie(root common.Hash) error { return nil }

func (np *noPruningTrieWriter) RejectTrie(root common.Hash) error { return nil }

func (np *noPruningTrieWriter) Shutdown() error { return nil }

type cappedMemoryTrieWriter struct {
	state.Database
	memoryCap                         common.StorageSize
	imageCap                          common.StorageSize
	lastAcceptedRoot                  common.Hash
	blocksAccepted, maxBlocksAccepted uint64
}

func (cm *cappedMemoryTrieWriter) InsertTrie(root common.Hash) error {
	triedb := cm.Database.TrieDB()
	triedb.Reference(root, common.Hash{})
	nodes, imgs := triedb.Size()

	if nodes > cm.memoryCap || imgs > cm.imageCap {
		return triedb.Cap(cm.memoryCap - ethdb.IdealBatchSize)
	}

	return nil
}

func (cm *cappedMemoryTrieWriter) AcceptTrie(root common.Hash) error {
	triedb := cm.Database.TrieDB()

	cm.blocksAccepted++
	cm.lastAcceptedRoot = root
	// If we haven't committed an accepted block root within the desired
	// interval make sure to commit this root.
	if cm.blocksAccepted > cm.maxBlocksAccepted {
		if err := triedb.Commit(root, false, nil); err != nil {
			return fmt.Errorf("failed to commit trie root %s: %w", root.Hex(), err)
		}
		cm.blocksAccepted = 0
	}

	// Cap the memory consumption by the dirty cache if it is exceeding
	// desired limit.
	nodes, imgs := triedb.Size()
	if nodes > cm.memoryCap || imgs > cm.imageCap {
		return triedb.Cap(cm.memoryCap - ethdb.IdealBatchSize)
	}

	return nil
}

func (cm *cappedMemoryTrieWriter) RejectTrie(root common.Hash) error {
	triedb := cm.Database.TrieDB()
	triedb.Dereference(root)
	return nil
}

func (cm *cappedMemoryTrieWriter) Shutdown() error {
	// If [lastAcceptedRoot] is empty, no need to do any cleanup on
	// shutdown.
	if cm.lastAcceptedRoot == (common.Hash{}) {
		return nil
	}

	// Attempt to commit [lastAcceptedRoot] on shutdown to avoid
	// re-processing the state on the next startup.
	triedb := cm.Database.TrieDB()
	return triedb.Commit(cm.lastAcceptedRoot, false, nil)
}
