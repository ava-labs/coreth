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
// Copyright 2017 The go-ethereum Authors
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

package state

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	ethstate "github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/coreth/triedb/firewood"
)

type (
	Database = ethstate.Database
	Trie     = ethstate.Trie
)

var (
	_ Database = (*firewoodAccessorDb)(nil)
	_ Trie     = (*firewood.AccountTrie)(nil)
	_ Trie     = (*firewood.StorageTrie)(nil)
)

func NewDatabase(db ethdb.Database) ethstate.Database {
	return ethstate.NewDatabase(db)
}

func NewDatabaseWithConfig(db ethdb.Database, config *triedb.Config) ethstate.Database {
	return ethstate.NewDatabaseWithConfig(db, config)
}

func NewDatabaseWithNodeDB(db ethdb.Database, triedb *triedb.Database) ethstate.Database {
	return ethstate.NewDatabaseWithNodeDB(db, triedb)
}

// Note trieConfig provides a closure here.
func NewDatabaseWithFirewood(db ethdb.Database, trieConfig *triedb.Config) Database {
	// I could create my own Config, create the config, and create the triedb.Config
	// Don't accept the triedb and create it here
	fw, ok := trieConfig.DBOverride(db).(*firewood.Database)
	if !ok {
		log.Error("Cannot create firewooddb.Database from the provided database")
		fmt.Printf("not ok")
		return nil
	}
	if fw == nil {
		fmt.Printf("firewood is nil")
		log.Error("firewooddb.Database is nil")
		return nil
	}

	return &firewoodAccessorDb{
		Database: ethstate.NewDatabaseWithConfig(db, trieConfig),
		fw:       fw,
	}
}

type firewoodAccessorDb struct {
	Database
	fw *firewood.Database
}

// OpenTrie opens the main account trie.
func (db *firewoodAccessorDb) OpenTrie(root common.Hash) (Trie, error) {
	return firewood.NewAccountTrie(root, db.fw)
}

// OpenStorageTrie opens the storage trie of an account.
func (db *firewoodAccessorDb) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, self Trie) (Trie, error) {
	accountTrie, ok := self.(*firewood.AccountTrie)
	if !ok {
		return nil, fmt.Errorf("Invalid account trie type: %T", self)
	}
	return firewood.NewStorageTrie(accountTrie, root)
}

// CopyTrie returns an independent copy of the given trie.
func (db *firewoodAccessorDb) CopyTrie(trie Trie) Trie {
	return nil
}
