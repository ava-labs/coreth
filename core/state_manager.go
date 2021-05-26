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
	"github.com/ava-labs/coreth/core/state"
	"github.com/ethereum/go-ethereum/common"
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
		return &acceptedBlockTrieWriter{
			Database: db,
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

type acceptedBlockTrieWriter struct {
	state.Database
	acceptedBlockCount          uint32
	periodicAcceptedBlockCommit uint32
}

func (ap *acceptedBlockTrieWriter) InsertTrie(root common.Hash) error {
	triedb := ap.Database.TrieDB()
	triedb.Reference(root, common.Hash{})
	return nil
}

func (ap *acceptedBlockTrieWriter) AcceptTrie(root common.Hash) error {
	triedb := ap.Database.TrieDB()
	return triedb.Commit(root, false, nil)
}

func (ap *acceptedBlockTrieWriter) RejectTrie(root common.Hash) error {
	triedb := ap.Database.TrieDB()
	triedb.Dereference(root)
	return nil
}

func (ap *acceptedBlockTrieWriter) Shutdown() error { return nil }

// TODO implement capability to undo a database commit by recursively deleting
// nodes from a root node. This will make more nuanced pruning strategies more
// performant.
