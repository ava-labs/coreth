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

// Package state provides a caching layer atop the Ethereum state trie.
package state

import (
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/common"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

func init() {
	// VERY IMPORTANT: This modifies the RLP encoding of the types.StateAccount
	// struct to include the IsMultiCoin field. This is necessary in coreth.
	gethtypes.MultiCoinEnabled = true

	trie.HashDefaults = &trie.Config{
		HashDB: hashdb.Defaults,
	}
}

type (
	Dump       = gethstate.Dump
	DumpConfig = gethstate.DumpConfig
)

// StateDB wraps gethstate.StateDB to provide additional functionality and
// modifications needed for this VM.
type StateDB struct {
	*gethstate.StateDB
	db Database
}

// New creates a new state from a given trie.
func New(root common.Hash, db Database, snaps SnapshotTree) (*StateDB, error) {
	if snaps == nil || checkNilInterface(snaps) {
		return NewWithSnapshot(root, db, nil, nil)
	}
	return NewWithSnapshot(root, db, snaps, snaps.Snapshot(root))
}

// NewWithSnapshot creates a new state from a given trie with the specified [snap]
// If [snap] doesn't have the same root as [root], then NewWithSnapshot will return
// an error.
func NewWithSnapshot(root common.Hash, db Database, snaps SnapshotTree, snap snapshot.Snapshot) (*StateDB, error) {
	statedb, err := gethstate.NewWithSnapshot(root, db, snaps, snap)
	if err != nil {
		return nil, err
	}
	return &StateDB{
		StateDB: statedb,
		db:      db,
	}, nil
}

// StartPrefetcher calls the StartPrefetcher method on the underlying StateDB.
// maxConcurrency is ignored.
// XXX: This breaks some performance expectations in coreth.
func (s *StateDB) StartPrefetcher(namespace string, maxConcurrency int) {
	s.StateDB.StartPrefetcher(namespace)
}

// AddLog adds a log with the specified parameters to the statedb
// Note: blockNumber is a required argument because StateDB does not
// know the current block number.
func (s *StateDB) AddLog(addr common.Address, topics []common.Hash, data []byte, blockNumber uint64) {
	s.StateDB.AddLog(&types.Log{
		Address:     addr,
		Topics:      topics,
		Data:        data,
		BlockNumber: blockNumber,
	})
}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	s.StateDB.Prepare(rules.AsGeth(), sender, coinbase, dst, precompiles, list)
}

// Copy creates a deep, independent copy of the state.
// Snapshots of the copied state cannot be applied to the copy.
func (s *StateDB) Copy() *StateDB {
	return &StateDB{StateDB: s.StateDB.Copy()}
}

func (s *StateDB) Database() Database {
	return s.db
}

func (s *StateDB) TrieDB() *trie.Database {
	return s.db.TrieDB()
}

func (s *StateDB) GetState(addr common.Address, key common.Hash) common.Hash {
	gethstate.NormalizeStateKey(&key)
	return s.StateDB.GetState(addr, key)
}

func (s *StateDB) SetState(addr common.Address, key common.Hash, value common.Hash) {
	gethstate.NormalizeStateKey(&key)
	s.StateDB.SetState(addr, key, value)
}
