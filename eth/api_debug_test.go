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

package eth

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/triedb"
	"github.com/davecgh/go-spew/spew"
	"github.com/holiman/uint256"
	"golang.org/x/exp/slices"
)

var dumper = spew.ConfigState{Indent: "    "}

func accountRangeTest(t *testing.T, trie *state.Trie, statedb *state.StateDB, start common.Hash, requestedNum int, expectedNum int) state.Dump {
	result := statedb.RawDump(&state.DumpConfig{
		SkipCode:          true,
		SkipStorage:       true,
		OnlyWithAddresses: false,
		Start:             start.Bytes(),
		Max:               uint64(requestedNum),
	})

	if len(result.Accounts) != expectedNum {
		t.Fatalf("expected %d results, got %d", expectedNum, len(result.Accounts))
	}
	for addr, acc := range result.Accounts {
		if strings.HasSuffix(addr, "pre") || acc.Address == nil {
			t.Fatalf("account without prestate (address) returned: %v", addr)
		}
		if !statedb.Exist(*acc.Address) {
			t.Fatalf("account not found in state %s", acc.Address.Hex())
		}
	}
	return result
}

func TestAccountRange(t *testing.T) {
	t.Parallel()

	var (
		statedb = extstate.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{Preimages: true})
		sdb, _  = state.New(types.EmptyRootHash, statedb, nil)
		addrs   = [AccountRangeMaxResults * 2]common.Address{}
		m       = map[common.Address]bool{}
	)

	for i := range addrs {
		hash := common.HexToHash(fmt.Sprintf("%x", i))
		addr := common.BytesToAddress(crypto.Keccak256Hash(hash.Bytes()).Bytes())
		addrs[i] = addr
		sdb.SetBalance(addrs[i], uint256.NewInt(1))
		if _, ok := m[addr]; ok {
			t.Fatalf("bad")
		} else {
			m[addr] = true
		}
	}
	root, _ := sdb.Commit(0, true)
	sdb, _ = state.New(root, statedb, nil)

	trie, err := statedb.OpenTrie(root)
	if err != nil {
		t.Fatal(err)
	}
	accountRangeTest(t, &trie, sdb, common.Hash{}, AccountRangeMaxResults/2, AccountRangeMaxResults/2)
	// test pagination
	firstResult := accountRangeTest(t, &trie, sdb, common.Hash{}, AccountRangeMaxResults, AccountRangeMaxResults)
	secondResult := accountRangeTest(t, &trie, sdb, common.BytesToHash(firstResult.Next), AccountRangeMaxResults, AccountRangeMaxResults)

	hList := make([]common.Hash, 0)
	for addr1, acc := range firstResult.Accounts {
		// If address is non-available, then it makes no sense to compare
		// them as they might be two different accounts.
		if acc.Address == nil {
			continue
		}
		if _, duplicate := secondResult.Accounts[addr1]; duplicate {
			t.Fatalf("pagination test failed:  results should not overlap")
		}
		hList = append(hList, crypto.Keccak256Hash(acc.Address.Bytes()))
	}
	// Test to see if it's possible to recover from the middle of the previous
	// set and get an even split between the first and second sets.
	slices.SortFunc(hList, common.Hash.Cmp)
	middleH := hList[AccountRangeMaxResults/2]
	middleResult := accountRangeTest(t, &trie, sdb, middleH, AccountRangeMaxResults, AccountRangeMaxResults)
	missing, infirst, insecond := 0, 0, 0
	for h := range middleResult.Accounts {
		if _, ok := firstResult.Accounts[h]; ok {
			infirst++
		} else if _, ok := secondResult.Accounts[h]; ok {
			insecond++
		} else {
			missing++
		}
	}
	if missing != 0 {
		t.Fatalf("%d hashes in the 'middle' set were neither in the first not the second set", missing)
	}
	if infirst != AccountRangeMaxResults/2 {
		t.Fatalf("Imbalance in the number of first-test results: %d != %d", infirst, AccountRangeMaxResults/2)
	}
	if insecond != AccountRangeMaxResults/2 {
		t.Fatalf("Imbalance in the number of second-test results: %d != %d", insecond, AccountRangeMaxResults/2)
	}
}

func TestEmptyAccountRange(t *testing.T) {
	t.Parallel()

	var (
		statedb = state.NewDatabase(rawdb.NewMemoryDatabase())
		st, _   = state.New(types.EmptyRootHash, statedb, nil)
	)
	// Commit(although nothing to flush) and re-init the statedb
	st.Commit(0, true)
	st, _ = state.New(types.EmptyRootHash, statedb, nil)

	results := st.RawDump(&state.DumpConfig{
		SkipCode:          true,
		SkipStorage:       true,
		OnlyWithAddresses: true,
		Max:               uint64(AccountRangeMaxResults),
	})
	if bytes.Equal(results.Next, (common.Hash{}).Bytes()) {
		t.Fatalf("Empty results should not return a second page")
	}
	if len(results.Accounts) != 0 {
		t.Fatalf("Empty state should not return addresses: %v", results.Accounts)
	}
}

func TestStorageRangeAt(t *testing.T) {
	t.Parallel()

	// Create a state where account 0x010000... has a few storage entries.
	var (
		db          = extstate.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{Preimages: true})
		sdb, _      = state.New(types.EmptyRootHash, db, nil)
		addr        = common.Address{0x01}
		keys        = []common.Hash{}
		storage     = storageMap{}
		storageList = []storageEntry{
			{Key: &common.Hash{0x00, 0x02}, Value: common.Hash{0x01}},
			{Key: &common.Hash{0x00, 0x04}, Value: common.Hash{0x02}},
			{Key: &common.Hash{0x00, 0x01}, Value: common.Hash{0x03}},
			{Key: &common.Hash{0x00, 0x03}, Value: common.Hash{0x04}},
		}
	)
	// Note: This test is modified compared to upstream since coreth normalizes
	// state keys before storing them. This means the original values cannot be
	// used, and the keys array and storage map must be re-calculated.
	for _, entry := range storageList {
		k := crypto.Keccak256Hash(entry.Key.Bytes())
		keys = append(keys, k)
		storage[k] = entry
	}
	slices.SortFunc(keys, common.Hash.Cmp)
	for _, entry := range storage {
		sdb.SetState(addr, *entry.Key, entry.Value)
	}
	root, _ := sdb.Commit(0, false)
	sdb, _ = state.New(root, db, nil)

	// Check a few combinations of limit and start/end.
	tests := []struct {
		start []byte
		limit int
		want  StorageRangeResult
	}{
		{
			start: []byte{}, limit: 0,
			want: StorageRangeResult{storageMap{}, &keys[0]},
		},
		{
			start: []byte{}, limit: 100,
			want: StorageRangeResult{storage, nil},
		},
		{
			start: []byte{}, limit: 2,
			want: StorageRangeResult{storageMap{keys[0]: storage[keys[0]], keys[1]: storage[keys[1]]}, &keys[2]},
		},
		{
			start: []byte{0x00}, limit: 4,
			want: StorageRangeResult{storage, nil},
		},
		{
			start: []byte{0x40}, limit: 2,
			want: StorageRangeResult{storageMap{keys[1]: storage[keys[1]], keys[2]: storage[keys[2]]}, &keys[3]},
		},
	}
	for _, test := range tests {
		result, err := storageRangeAt(sdb, root, addr, test.start, test.limit)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(result, test.want) {
			t.Fatalf("wrong result for range %#x.., limit %d:\ngot %s\nwant %s",
				test.start, test.limit, dumper.Sdump(result), dumper.Sdump(&test.want))
		}
	}
}
