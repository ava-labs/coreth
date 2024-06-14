// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie/triedb/hashdb"
	"github.com/ava-labs/coreth/trie/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

// Tests that updating a state trie does not leak any database writes prior to
// actually committing the state.
func TestUpdateLeaks(t *testing.T) {
	// Create an empty state database
	var (
		db  = rawdb.NewMemoryDatabase()
		tdb = trie.NewDatabase(db, nil)
	)
	state, _ := New(types.EmptyRootHash, NewDatabaseWithNodeDB(db, tdb), nil)

	// Update it with some accounts
	for i := byte(0); i < 255; i++ {
		addr := common.BytesToAddress([]byte{i})
		state.AddBalance(addr, big.NewInt(int64(11*i)))
		state.SetNonce(addr, uint64(42*i))
		if i%2 == 0 {
			state.SetState(addr, common.BytesToHash([]byte{i, i, i}), common.BytesToHash([]byte{i, i, i, i}))
		}
		if i%3 == 0 {
			state.SetCode(addr, []byte{i, i, i, i, i})
		}
	}

	root := state.IntermediateRoot(false)
	if err := tdb.Commit(root, false); err != nil {
		t.Errorf("can not commit trie %v to persistent database", root.Hex())
	}

	// Ensure that no data was leaked into the database
	it := db.NewIterator(nil, nil)
	for it.Next() {
		t.Errorf("State leaked into database: %x -> %x", it.Key(), it.Value())
	}
	it.Release()
}

// Tests that no intermediate state of an object is stored into the database,
// only the one right before the commit.
func TestIntermediateLeaks(t *testing.T) {
	// Create two state databases, one transitioning to the final state, the other final from the beginning
	transDb := rawdb.NewMemoryDatabase()
	finalDb := rawdb.NewMemoryDatabase()
	transNdb := trie.NewDatabase(transDb, nil)
	finalNdb := trie.NewDatabase(finalDb, nil)
	transState, _ := New(types.EmptyRootHash, NewDatabaseWithNodeDB(transDb, transNdb), nil)
	finalState, _ := New(types.EmptyRootHash, NewDatabaseWithNodeDB(finalDb, finalNdb), nil)

	modify := func(state *StateDB, addr common.Address, i, tweak byte) {
		state.SetBalance(addr, big.NewInt(int64(11*i)+int64(tweak)))
		state.SetNonce(addr, uint64(42*i+tweak))
		if i%2 == 0 {
			state.SetState(addr, common.Hash{i, i, i, 0}, common.Hash{})
			state.SetState(addr, common.Hash{i, i, i, tweak}, common.Hash{i, i, i, i, tweak})
		}
		if i%3 == 0 {
			state.SetCode(addr, []byte{i, i, i, i, i, tweak})
		}
	}

	// Modify the transient state.
	for i := byte(0); i < 255; i++ {
		modify(transState, common.Address{i}, i, 0)
	}
	// Write modifications to trie.
	transState.IntermediateRoot(false)

	// Overwrite all the data with new values in the transient database.
	for i := byte(0); i < 255; i++ {
		modify(transState, common.Address{i}, i, 99)
		modify(finalState, common.Address{i}, i, 99)
	}

	// Commit and cross check the databases.
	transRoot, err := transState.Commit(0, false)
	if err != nil {
		t.Fatalf("failed to commit transition state: %v", err)
	}
	if err = transNdb.Commit(transRoot, false); err != nil {
		t.Errorf("can not commit trie %v to persistent database", transRoot.Hex())
	}

	finalRoot, err := finalState.Commit(0, false)
	if err != nil {
		t.Fatalf("failed to commit final state: %v", err)
	}
	if err = finalNdb.Commit(finalRoot, false); err != nil {
		t.Errorf("can not commit trie %v to persistent database", finalRoot.Hex())
	}

	it := finalDb.NewIterator(nil, nil)
	for it.Next() {
		key, fvalue := it.Key(), it.Value()
		tvalue, err := transDb.Get(key)
		if err != nil {
			t.Errorf("entry missing from the transition database: %x -> %x", key, fvalue)
		}
		if !bytes.Equal(fvalue, tvalue) {
			t.Errorf("value mismatch at key %x: %x in transition database, %x in final database", key, tvalue, fvalue)
		}
	}
	it.Release()

	it = transDb.NewIterator(nil, nil)
	for it.Next() {
		key, tvalue := it.Key(), it.Value()
		fvalue, err := finalDb.Get(key)
		if err != nil {
			t.Errorf("extra entry in the transition database: %x -> %x", key, it.Value())
		}
		if !bytes.Equal(fvalue, tvalue) {
			t.Errorf("value mismatch at key %x: %x in transition database, %x in final database", key, tvalue, fvalue)
		}
	}
}

// A snapshotTest checks that reverting StateDB snapshots properly undoes all changes
// captured by the snapshot. Instances of this test with pseudorandom content are created
// by Generate.
//
// The test works as follows:
//
// A new state is created and all actions are applied to it. Several snapshots are taken
// in between actions. The test then reverts each snapshot. For each snapshot the actions
// leading up to it are replayed on a fresh, empty state. The behaviour of all public
// accessor methods on the reverted state must match the return value of the equivalent
// methods on the replayed state.
type snapshotTest struct {
	addrs     []common.Address // all account addresses
	actions   []testAction     // modifications to the state
	snapshots []int            // actions indexes at which snapshot is taken
	err       error            // failure details are reported through this field
}

type testAction struct {
	name   string
	fn     func(testAction, *StateDB)
	args   []int64
	noAddr bool
}

// newTestAction creates a random action that changes state.
func newTestAction(addr common.Address, r *rand.Rand) testAction {
	actions := []testAction{
		{
			name: "SetBalance",
			fn: func(a testAction, s *StateDB) {
				s.SetBalance(addr, big.NewInt(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "AddBalance",
			fn: func(a testAction, s *StateDB) {
				s.AddBalance(addr, big.NewInt(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "SetNonce",
			fn: func(a testAction, s *StateDB) {
				s.SetNonce(addr, uint64(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "SetState",
			fn: func(a testAction, s *StateDB) {
				var key, val common.Hash
				binary.BigEndian.PutUint16(key[:], uint16(a.args[0]))
				binary.BigEndian.PutUint16(val[:], uint16(a.args[1]))
				s.SetState(addr, key, val)
			},
			args: make([]int64, 2),
		},
		{
			name: "SetCode",
			fn: func(a testAction, s *StateDB) {
				code := make([]byte, 16)
				binary.BigEndian.PutUint64(code, uint64(a.args[0]))
				binary.BigEndian.PutUint64(code[8:], uint64(a.args[1]))
				s.SetCode(addr, code)
			},
			args: make([]int64, 2),
		},
		{
			name: "CreateAccount",
			fn: func(a testAction, s *StateDB) {
				s.CreateAccount(addr)
			},
		},
		{
			name: "SelfDestruct",
			fn: func(a testAction, s *StateDB) {
				s.SelfDestruct(addr)
			},
		},
		{
			name: "AddRefund",
			fn: func(a testAction, s *StateDB) {
				s.AddRefund(uint64(a.args[0]))
			},
			args:   make([]int64, 1),
			noAddr: true,
		},
		{
			name: "AddLog",
			fn: func(a testAction, s *StateDB) {
				data := make([]byte, 2)
				binary.BigEndian.PutUint16(data, uint16(a.args[0]))
				s.AddLog(addr, nil, data, 0)
			},
			args: make([]int64, 1),
		},
		{
			name: "AddPreimage",
			fn: func(a testAction, s *StateDB) {
				preimage := []byte{1}
				hash := common.BytesToHash(preimage)
				s.AddPreimage(hash, preimage)
			},
			args: make([]int64, 1),
		},
		{
			name: "AddAddressToAccessList",
			fn: func(a testAction, s *StateDB) {
				s.AddAddressToAccessList(addr)
			},
		},
		{
			name: "AddSlotToAccessList",
			fn: func(a testAction, s *StateDB) {
				s.AddSlotToAccessList(addr,
					common.Hash{byte(a.args[0])})
			},
			args: make([]int64, 1),
		},
		{
			name: "SetTransientState",
			fn: func(a testAction, s *StateDB) {
				var key, val common.Hash
				binary.BigEndian.PutUint16(key[:], uint16(a.args[0]))
				binary.BigEndian.PutUint16(val[:], uint16(a.args[1]))
				s.SetTransientState(addr, key, val)
			},
			args: make([]int64, 2),
		},
	}
	action := actions[r.Intn(len(actions))]
	var nameargs []string
	if !action.noAddr {
		nameargs = append(nameargs, addr.Hex())
	}
	for i := range action.args {
		action.args[i] = rand.Int63n(100)
		nameargs = append(nameargs, fmt.Sprint(action.args[i]))
	}
	action.name += strings.Join(nameargs, ", ")
	return action
}

// Generate returns a new snapshot test of the given size. All randomness is
// derived from r.
func (*snapshotTest) Generate(r *rand.Rand, size int) reflect.Value {
	// Generate random actions.
	addrs := make([]common.Address, 50)
	for i := range addrs {
		addrs[i][0] = byte(i)
	}
	actions := make([]testAction, size)
	for i := range actions {
		addr := addrs[r.Intn(len(addrs))]
		actions[i] = newTestAction(addr, r)
	}
	// Generate snapshot indexes.
	nsnapshots := int(math.Sqrt(float64(size)))
	if size > 0 && nsnapshots == 0 {
		nsnapshots = 1
	}
	snapshots := make([]int, nsnapshots)
	snaplen := len(actions) / nsnapshots
	for i := range snapshots {
		// Try to place the snapshots some number of actions apart from each other.
		snapshots[i] = (i * snaplen) + r.Intn(snaplen)
	}
	return reflect.ValueOf(&snapshotTest{addrs, actions, snapshots, nil})
}

func (test *snapshotTest) String() string {
	out := new(bytes.Buffer)
	sindex := 0
	for i, action := range test.actions {
		if len(test.snapshots) > sindex && i == test.snapshots[sindex] {
			fmt.Fprintf(out, "---- snapshot %d ----\n", sindex)
			sindex++
		}
		fmt.Fprintf(out, "%4d: %s\n", i, action.name)
	}
	return out.String()
}

// TestCopyOfCopy tests that modified objects are carried over to the copy, and the copy of the copy.
// See https://github.com/ethereum/go-ethereum/pull/15225#issuecomment-380191512
func TestCopyOfCopy(t *testing.T) {
	state, _ := New(types.EmptyRootHash, NewDatabase(rawdb.NewMemoryDatabase()), nil)
	addr := common.HexToAddress("aaaa")
	state.SetBalance(addr, big.NewInt(42))

	if got := state.Copy().GetBalance(addr).Uint64(); got != 42 {
		t.Fatalf("1st copy fail, expected 42, got %v", got)
	}
	if got := state.Copy().Copy().GetBalance(addr).Uint64(); got != 42 {
		t.Fatalf("2nd copy fail, expected 42, got %v", got)
	}
}

// Tests a regression where committing a copy lost some internal meta information,
// leading to corrupted subsequent copies.
//
// See https://github.com/ethereum/go-ethereum/issues/20106.
func TestCopyCommitCopy(t *testing.T) {
	tdb := NewDatabase(rawdb.NewMemoryDatabase())
	state, _ := New(types.EmptyRootHash, tdb, nil)

	// Create an account and check if the retrieved balance is correct
	addr := common.HexToAddress("0xaffeaffeaffeaffeaffeaffeaffeaffeaffeaffe")
	skey := common.HexToHash("aaa")
	sval := common.HexToHash("bbb")

	state.SetBalance(addr, big.NewInt(42)) // Change the account trie
	state.SetCode(addr, []byte("hello"))   // Change an external metadata
	state.SetState(addr, skey, sval)       // Change the storage trie

	if balance := state.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("initial balance mismatch: have %v, want %v", balance, 42)
	}
	if code := state.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("initial code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := state.GetState(addr, skey); val != sval {
		t.Fatalf("initial non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := state.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("initial committed storage slot mismatch: have %x, want %x", val, common.Hash{})
	}
	// Copy the non-committed state database and check pre/post commit balance
	copyOne := state.Copy()
	if balance := copyOne.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("first copy pre-commit balance mismatch: have %v, want %v", balance, 42)
	}
	if code := copyOne.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("first copy pre-commit code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := copyOne.GetState(addr, skey); val != sval {
		t.Fatalf("first copy pre-commit non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := copyOne.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("first copy pre-commit committed storage slot mismatch: have %x, want %x", val, common.Hash{})
	}
	// Copy the copy and check the balance once more
	copyTwo := copyOne.Copy()
	if balance := copyTwo.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("second copy balance mismatch: have %v, want %v", balance, 42)
	}
	if code := copyTwo.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("second copy code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := copyTwo.GetState(addr, skey); val != sval {
		t.Fatalf("second copy non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := copyTwo.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("second copy committed storage slot mismatch: have %x, want %x", val, sval)
	}
	// Commit state, ensure states can be loaded from disk
	root, _ := state.Commit(0, false)
	state, _ = New(root, tdb, nil)
	if balance := state.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("state post-commit balance mismatch: have %v, want %v", balance, 42)
	}
	if code := state.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("state post-commit code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := state.GetState(addr, skey); val != sval {
		t.Fatalf("state post-commit non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := state.GetCommittedState(addr, skey); val != sval {
		t.Fatalf("state post-commit committed storage slot mismatch: have %x, want %x", val, sval)
	}
}

// Tests a regression where committing a copy lost some internal meta information,
// leading to corrupted subsequent copies.
//
// See https://github.com/ethereum/go-ethereum/issues/20106.
func TestCopyCopyCommitCopy(t *testing.T) {
	state, _ := New(types.EmptyRootHash, NewDatabase(rawdb.NewMemoryDatabase()), nil)

	// Create an account and check if the retrieved balance is correct
	addr := common.HexToAddress("0xaffeaffeaffeaffeaffeaffeaffeaffeaffeaffe")
	skey := common.HexToHash("aaa")
	sval := common.HexToHash("bbb")

	state.SetBalance(addr, big.NewInt(42)) // Change the account trie
	state.SetCode(addr, []byte("hello"))   // Change an external metadata
	state.SetState(addr, skey, sval)       // Change the storage trie

	if balance := state.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("initial balance mismatch: have %v, want %v", balance, 42)
	}
	if code := state.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("initial code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := state.GetState(addr, skey); val != sval {
		t.Fatalf("initial non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := state.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("initial committed storage slot mismatch: have %x, want %x", val, common.Hash{})
	}
	// Copy the non-committed state database and check pre/post commit balance
	copyOne := state.Copy()
	if balance := copyOne.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("first copy balance mismatch: have %v, want %v", balance, 42)
	}
	if code := copyOne.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("first copy code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := copyOne.GetState(addr, skey); val != sval {
		t.Fatalf("first copy non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := copyOne.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("first copy committed storage slot mismatch: have %x, want %x", val, common.Hash{})
	}
	// Copy the copy and check the balance once more
	copyTwo := copyOne.Copy()
	if balance := copyTwo.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("second copy pre-commit balance mismatch: have %v, want %v", balance, 42)
	}
	if code := copyTwo.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("second copy pre-commit code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := copyTwo.GetState(addr, skey); val != sval {
		t.Fatalf("second copy pre-commit non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := copyTwo.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("second copy pre-commit committed storage slot mismatch: have %x, want %x", val, common.Hash{})
	}
	// Copy the copy-copy and check the balance once more
	copyThree := copyTwo.Copy()
	if balance := copyThree.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("third copy balance mismatch: have %v, want %v", balance, 42)
	}
	if code := copyThree.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("third copy code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := copyThree.GetState(addr, skey); val != sval {
		t.Fatalf("third copy non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := copyThree.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("third copy committed storage slot mismatch: have %x, want %x", val, sval)
	}
}

// TestCommitCopy tests the copy from a committed state is not functional.
func TestCommitCopy(t *testing.T) {
	state, _ := New(types.EmptyRootHash, NewDatabase(rawdb.NewMemoryDatabase()), nil)

	// Create an account and check if the retrieved balance is correct
	addr := common.HexToAddress("0xaffeaffeaffeaffeaffeaffeaffeaffeaffeaffe")
	skey := common.HexToHash("aaa")
	sval := common.HexToHash("bbb")

	state.SetBalance(addr, big.NewInt(42)) // Change the account trie
	state.SetCode(addr, []byte("hello"))   // Change an external metadata
	state.SetState(addr, skey, sval)       // Change the storage trie

	if balance := state.GetBalance(addr); balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("initial balance mismatch: have %v, want %v", balance, 42)
	}
	if code := state.GetCode(addr); !bytes.Equal(code, []byte("hello")) {
		t.Fatalf("initial code mismatch: have %x, want %x", code, []byte("hello"))
	}
	if val := state.GetState(addr, skey); val != sval {
		t.Fatalf("initial non-committed storage slot mismatch: have %x, want %x", val, sval)
	}
	if val := state.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("initial committed storage slot mismatch: have %x, want %x", val, common.Hash{})
	}
	// Copy the committed state database, the copied one is not functional.
	state.Commit(0, true)
	copied := state.Copy()
	if balance := copied.GetBalance(addr); balance.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("unexpected balance: have %v", balance)
	}
	if code := copied.GetCode(addr); code != nil {
		t.Fatalf("unexpected code: have %x", code)
	}
	if val := copied.GetState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("unexpected storage slot: have %x", val)
	}
	if val := copied.GetCommittedState(addr, skey); val != (common.Hash{}) {
		t.Fatalf("unexpected storage slot: have %x", val)
	}
	if !errors.Is(copied.Error(), trie.ErrCommitted) {
		t.Fatalf("unexpected state error, %v", copied.Error())
	}
}

// TestMissingTrieNodes tests that if the StateDB fails to load parts of the trie,
// the Commit operation fails with an error
// If we are missing trie nodes, we should not continue writing to the trie
func TestMissingTrieNodes(t *testing.T) {
	testMissingTrieNodes(t, rawdb.HashScheme)
	testMissingTrieNodes(t, rawdb.PathScheme)
}

func testMissingTrieNodes(t *testing.T, scheme string) {
	// Create an initial state with a few accounts
	var (
		triedb *trie.Database
		memDb  = rawdb.NewMemoryDatabase()
	)
	if scheme == rawdb.PathScheme {
		triedb = trie.NewDatabase(memDb, &trie.Config{PathDB: &pathdb.Config{
			CleanCacheSize: 0,
			DirtyCacheSize: 0,
		}}) // disable caching
	} else {
		triedb = trie.NewDatabase(memDb, &trie.Config{HashDB: &hashdb.Config{
			CleanCacheSize: 0,
		}}) // disable caching
	}
	db := NewDatabaseWithNodeDB(memDb, triedb)

	var root common.Hash
	state, _ := New(types.EmptyRootHash, db, nil)
	addr := common.BytesToAddress([]byte("so"))
	{
		state.SetBalance(addr, big.NewInt(1))
		state.SetCode(addr, []byte{1, 2, 3})
		a2 := common.BytesToAddress([]byte("another"))
		state.SetBalance(a2, big.NewInt(100))
		state.SetCode(a2, []byte{1, 2, 4})
		root, _ = state.Commit(0, false)
		t.Logf("root: %x", root)
		// force-flush
		triedb.Commit(root, false)
	}
	// Create a new state on the old root
	state, _ = New(root, db, nil)
	// Now we clear out the memdb
	it := memDb.NewIterator(nil, nil)
	for it.Next() {
		k := it.Key()
		// Leave the root intact
		if !bytes.Equal(k, root[:]) {
			t.Logf("key: %x", k)
			memDb.Delete(k)
		}
	}
	balance := state.GetBalance(addr)
	// The removed elem should lead to it returning zero balance
	if exp, got := uint64(0), balance.Uint64(); got != exp {
		t.Errorf("expected %d, got %d", exp, got)
	}
	// Modify the state
	state.SetBalance(addr, big.NewInt(2))
	root, err := state.Commit(0, false)
	if err == nil {
		t.Fatalf("expected error, got root :%x", root)
	}
}

func TestMultiCoinSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sdb := NewDatabase(db)

	// Create empty snapshot.Tree and StateDB
	root := common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	// Use the root as both the stateRoot and blockHash for this test.
	snapTree := snapshot.NewTestTree(db, root, root)

	addr := common.Address{1}
	assetID1 := common.Hash{1}
	assetID2 := common.Hash{2}

	var stateDB *StateDB
	assertBalances := func(regular, multicoin1, multicoin2 int64) {
		balance := stateDB.GetBalance(addr)
		if balance.Cmp(big.NewInt(regular)) != 0 {
			t.Fatal("incorrect non-multicoin balance")
		}
		balance = stateDB.GetBalanceMultiCoin(addr, assetID1)
		if balance.Cmp(big.NewInt(multicoin1)) != 0 {
			t.Fatal("incorrect multicoin1 balance")
		}
		balance = stateDB.GetBalanceMultiCoin(addr, assetID2)
		if balance.Cmp(big.NewInt(multicoin2)) != 0 {
			t.Fatal("incorrect multicoin2 balance")
		}
	}

	// Create new state
	stateDB, _ = New(root, sdb, snapTree)
	assertBalances(0, 0, 0)

	stateDB.AddBalance(addr, big.NewInt(10))
	assertBalances(10, 0, 0)

	// Commit and get the new root
	root, _ = stateDB.Commit(0, false)
	assertBalances(10, 0, 0)

	// Create a new state from the latest root, add a multicoin balance, and
	// commit it to the tree.
	stateDB, _ = New(root, sdb, snapTree)
	stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(10))
	root, _ = stateDB.Commit(0, false)
	assertBalances(10, 10, 0)

	// Add more layers than the cap and ensure the balances and layers are correct
	for i := 0; i < 256; i++ {
		stateDB, _ = New(root, sdb, snapTree)
		stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
		stateDB.AddBalanceMultiCoin(addr, assetID2, big.NewInt(2))
		root, _ = stateDB.Commit(0, false)
	}
	assertBalances(10, 266, 512)

	// Do one more add, including the regular balance which is now in the
	// collapsed snapshot
	stateDB, _ = New(root, sdb, snapTree)
	stateDB.AddBalance(addr, big.NewInt(1))
	stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
	root, _ = stateDB.Commit(0, false)
	stateDB, _ = New(root, sdb, snapTree)
	assertBalances(11, 267, 512)
}

func TestGenerateMultiCoinAccounts(t *testing.T) {
	var (
		diskdb   = rawdb.NewMemoryDatabase()
		database = NewDatabase(diskdb)

		addr     = common.BytesToAddress([]byte("addr1"))
		addrHash = crypto.Keccak256Hash(addr[:])

		assetID      = common.BytesToHash([]byte("coin1"))
		assetBalance = big.NewInt(10)
	)

	stateDB, err := New(common.Hash{}, database, nil)
	if err != nil {
		t.Fatal(err)
	}
	stateDB.SetBalanceMultiCoin(addr, assetID, assetBalance)
	root, err := stateDB.Commit(0, false)
	if err != nil {
		t.Fatal(err)
	}

	triedb := database.TrieDB()
	if err := triedb.Commit(root, true); err != nil {
		t.Fatal(err)
	}
	// Build snapshot from scratch
	snapConfig := snapshot.Config{
		CacheSize:  16,
		AsyncBuild: false,
		NoBuild:    false,
		SkipVerify: true,
	}
	snaps, err := snapshot.New(snapConfig, diskdb, triedb, common.Hash{}, root)
	if err != nil {
		t.Error("Unexpected error while rebuilding snapshot:", err)
	}

	// Get latest snapshot and make sure it has the correct account and storage
	snap := snaps.Snapshot(root)
	snapAccount, err := snap.Account(addrHash)
	if err != nil {
		t.Fatal(err)
	}
	if !snapAccount.IsMultiCoin {
		t.Fatalf("Expected SnapAccount to return IsMultiCoin: true, found: %v", snapAccount.IsMultiCoin)
	}

	gethstate.NormalizeCoinID(&assetID)
	assetHash := crypto.Keccak256Hash(assetID.Bytes())
	storageBytes, err := snap.Storage(addrHash, assetHash)
	if err != nil {
		t.Fatal(err)
	}

	actualAssetBalance := new(big.Int).SetBytes(storageBytes)
	if actualAssetBalance.Cmp(assetBalance) != 0 {
		t.Fatalf("Expected asset balance: %v, found %v", assetBalance, actualAssetBalance)
	}
}

// Tests that account and storage tries are flushed in the correct order and that
// no data loss occurs.
func TestFlushOrderDataLoss(t *testing.T) {
	// Create a state trie with many accounts and slots
	var (
		memdb    = rawdb.NewMemoryDatabase()
		triedb   = trie.NewDatabase(memdb, nil)
		statedb  = NewDatabaseWithNodeDB(memdb, triedb)
		state, _ = New(types.EmptyRootHash, statedb, nil)
	)
	for a := byte(0); a < 10; a++ {
		state.CreateAccount(common.Address{a})
		for s := byte(0); s < 10; s++ {
			state.SetState(common.Address{a}, common.Hash{a, s}, common.Hash{a, s})
		}
	}
	root, err := state.Commit(0, false)
	if err != nil {
		t.Fatalf("failed to commit state trie: %v", err)
	}
	triedb.Reference(root, common.Hash{})
	if err := triedb.Cap(1024); err != nil {
		t.Fatalf("failed to cap trie dirty cache: %v", err)
	}
	if err := triedb.Commit(root, false); err != nil {
		t.Fatalf("failed to commit state trie: %v", err)
	}
	// Reopen the state trie from flushed disk and verify it
	state, err = New(root, NewDatabase(memdb), nil)
	if err != nil {
		t.Fatalf("failed to reopen state trie: %v", err)
	}
	for a := byte(0); a < 10; a++ {
		for s := byte(0); s < 10; s++ {
			if have := state.GetState(common.Address{a}, common.Hash{a, s}); have != (common.Hash{a, s}) {
				t.Errorf("account %d: slot %d: state mismatch: have %x, want %x", a, s, have, common.Hash{a, s})
			}
		}
	}
}

func TestResetObject(t *testing.T) {
	var (
		disk     = rawdb.NewMemoryDatabase()
		tdb      = trie.NewDatabase(disk, nil)
		db       = NewDatabaseWithNodeDB(disk, tdb)
		snaps, _ = snapshot.New(snapshot.Config{CacheSize: 10}, disk, tdb, common.Hash{}, types.EmptyRootHash)
		state, _ = New(types.EmptyRootHash, db, snaps)
		addr     = common.HexToAddress("0x1")
		slotA    = common.HexToHash("0x1")
		slotB    = common.HexToHash("0x2")
	)
	// Initialize account with balance and storage in first transaction.
	state.SetBalance(addr, big.NewInt(1))
	state.SetState(addr, slotA, common.BytesToHash([]byte{0x1}))
	state.IntermediateRoot(true)

	// Reset account and mutate balance and storages
	state.CreateAccount(addr)
	state.SetBalance(addr, big.NewInt(2))
	state.SetState(addr, slotB, common.BytesToHash([]byte{0x2}))
	root, _ := state.Commit(0, true)

	// Ensure the original account is wiped properly
	snap := snaps.Snapshot(root)
	slot, _ := snap.Storage(crypto.Keccak256Hash(addr.Bytes()), crypto.Keccak256Hash(slotA.Bytes()))
	if len(slot) != 0 {
		t.Fatalf("Unexpected storage slot")
	}
	slot, _ = snap.Storage(crypto.Keccak256Hash(addr.Bytes()), crypto.Keccak256Hash(slotB.Bytes()))
	if !bytes.Equal(slot, []byte{0x2}) {
		t.Fatalf("Unexpected storage slot value %v", slot)
	}
}
