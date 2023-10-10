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

package trie

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultTestMaxGoroutines = 10

func TestTrieBatchGetEmpty(t *testing.T) {
	require := require.New(t)
	trie := NewEmpty(NewDatabase(rawdb.NewMemoryDatabase()))
	res := trie.Hash()
	exp := types.EmptyRootHash
	require.Equal(exp, res)

	if err := trie.BatchGet(defaultTestMaxGoroutines, [][]byte{}); err != nil {
		t.Fatal(err)
	}

	if err := trie.BatchGet(defaultTestMaxGoroutines, [][]byte{{1}}); err != nil {
		t.Fatal(err)
	}

	if err := trie.BatchGet(defaultTestMaxGoroutines, [][]byte{{1}, {0, 1}, {0, 0}, {0, 1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
}

func TestTrieBatchGetSimple(t *testing.T) {
	testTrieBatchGetExpands(t, [][]byte{{0x00}, {0x01}}, [][]byte{{0x00}, {0x01}})
}

func TestTrieBatchGetSingleKey(t *testing.T) {
	testTrieBatchGetExpands(t, [][]byte{{0x00}}, [][]byte{{0x01}})
}

func TestTrieBatchGetDifferentSizeKeys(t *testing.T) {
	keys := [][]byte{
		{0, 1, 2, 3},
		{0, 0, 0, 0},
		{0, 0},
		{1, 0, 1},
		{0, 1, 1, 2, 3, 4},
	}
	vals := generateKeys(len(keys), false)
	testTrieBatchGetExpands(t, keys, vals)
}

func TestTrieBatchGetIncreasingNumKeys(t *testing.T) {
	testTrieBatchGetExpands(t, generateKeys(10, false), generateKeys(10, false))
	testTrieBatchGetExpands(t, generateKeys(100, false), generateKeys(100, false))
	testTrieBatchGetExpands(t, generateKeys(100, true), generateKeys(100, false))
	testTrieBatchGetExpands(t, generateKeys(100, true), generateKeys(100, true))
	testTrieBatchGetExpands(t, generateKeys(1000, true), generateKeys(1000, true))
	testTrieBatchGetExpands(t, generateKeys(10000, true), generateKeys(10000, true))
}

func TestTrieBatchGetMixedLengthKeys(t *testing.T) {
	testTrieBatchGetExpands(t, [][]byte{{0x00, 0x00}, {0x00, 0x00, 0x01}, {0x00}}, [][]byte{{0x01}, {0x01}, {0x00}})
	testTrieBatchGetExpands(t, [][]byte{{0x01, 0x00}, {0x00, 0x00, 0x01}, {0x00}}, [][]byte{{0x01}, {0x01}, {0x00}})
	testTrieBatchGetExpands(t, [][]byte{{0x01, 0x00}, {0x01, 0x00, 0x01}, {0x00}}, [][]byte{{0x01}, {0x01}, {0x00}})
	testTrieBatchGetExpands(t, [][]byte{{0x01, 0x00, 0x00}, {0x01, 0x00, 0x01}, {0x00, 0x01, 0x00}}, [][]byte{{0x01}, {0x01}, {0x01}})
}

func TestTrieBatchGetMixedLengthKeysZeroLenVals(t *testing.T) {
	testTrieBatchGetExpands(t, [][]byte{{0x00, 0x00}, {0x00, 0x00, 0x01}, {0x00}}, [][]byte{{}, {0x01}, {0x00}})
	testTrieBatchGetExpands(t, [][]byte{{0x01, 0x00}, {0x00, 0x00, 0x01}, {0x00}}, [][]byte{{0x01}, nil, {0x00}})
	testTrieBatchGetExpands(t, [][]byte{{0x01, 0x00}, {0x01, 0x00, 0x01}, {0x00}}, [][]byte{nil, {0x01}, {0x00}})
	testTrieBatchGetExpands(t, [][]byte{{0x01, 0x00, 0x00}, {0x01, 0x00, 0x01}, {0x00, 0x01, 0x00}}, [][]byte{{0x01}, nil, {0x01}})
}

func FuzzTrieBatchGetExpands(f *testing.F) {
	f.Fuzz(testTrieBatchGetExpandsFromSeed)
}

func TestTrieBatchGetExpands(t *testing.T) {
	testTrieBatchGetExpandsFromSeed(t, -93)
}

func TestTrieBatchRegression(t *testing.T) {
	testTrieBatchGetExpands(t, [][]byte{{215, 127, 206, 64}, {216, 219, 17, 173}, {185}, {0, 88}}, [][]byte{{135}, {233}, {}, {}})
}

func testTrieBatchGetExpandsFromSeed(t *testing.T, seed int64) {
	require := require.New(t)

	maxKeyLength := 8
	maxValueLength := 2
	rand.Seed(seed)

	numKeyValuePairs := rand.Intn(10_000)
	t.Log("numKeyValuePairs", numKeyValuePairs)

	keyValsMap := make(map[string][]byte)
	for i := 0; i < numKeyValuePairs; i++ {
		keyLen := 1 + rand.Intn(maxKeyLength)
		valLen := rand.Intn(maxValueLength)
		key := make([]byte, keyLen)
		_, err := rand.Read(key)
		require.NoError(err)
		val := make([]byte, valLen)
		_, err = rand.Read(val)
		require.NoError(err)
		keyValsMap[string(key)] = val
	}

	keys := make([][]byte, 0, len(keyValsMap))
	values := make([][]byte, 0, len(keyValsMap))

	for keyStr, val := range keyValsMap {
		keys = append(keys, []byte(keyStr))
		values = append(values, val)
	}
	t.Log("Keys:\n", keys)
	t.Log("Values:\n", values)

	testTrieBatchGetExpands(t, keys, values)
}

func checkTrie(assert *assert.Assertions, trie *Trie, keys [][]byte, values [][]byte) {
	for i, key := range keys {
		val, err := trie.Get(key)
		assert.NoError(err)
		if len(values[i]) == 0 {
			assert.Nil(val)
		} else {
			assert.Equal(values[i], val)
		}
	}
}

func testTrieBatchGetExpands(t testing.TB, keys, values [][]byte) {
	assert := assert.New(t)

	assert.Equal(len(keys), len(values))
	db := NewDatabase(rawdb.NewMemoryDatabase())
	trie := NewEmpty(db)
	res := trie.Hash()
	exp := types.EmptyRootHash
	assert.Equal(exp, res)

	for i, key := range keys {
		assert.NoError(trie.Update(key, values[i]))
	}

	_, nodes := trie.Commit(true)
	if nodes == nil {
		t.Skip("nil nodes")
	}

	rootHashNode, ok := trie.root.(hashNode)
	assert.True(ok)

	// Re-construct trie with hash node at the root and perform BatchGet to force expand
	trie = &Trie{root: rootHashNode, reader: &trieReader{reader: nodes}, tracer: newTracer()}
	assert.NoError(trie.BatchGet(defaultTestMaxGoroutines, keys))

	// The current implementation of BatchGet does not return anything (makes my life easier)
	// The purpose of BatchGet is to read all of the trie nodes from disk and expand the trie in-memory
	// To test it out, we can replace the trie reader with an empty reader and then check again that the correct values
	// are present.
	trie.reader = newEmptyReader()

	checkTrie(assert, trie, keys, values)
}
