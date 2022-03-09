// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trie

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

// GenerateTrie creates a trie inside of [trieDB] coming from a deterministic random seed
// with a total of [numKeys] key-value pairs.
// Returns the root of the generated trie, the slice of keys inserted into the trie in lexicographical
// order and the slice of corresponding values.
func GenerateTrie(t *testing.T, trieDB *Database, numKeys int) (common.Hash, [][]byte, [][]byte) {
	testTrie, err := New(common.Hash{}, trieDB)
	assert.NoError(t, err)

	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Set random seed for repeatable outcome
	rand.Seed(1)

	// Generate key-value pairs
	for i := 0; i < numKeys; i++ {
		key := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(key, uint64(i))

		value := make([]byte, rand.Intn(128)+128) // min 128 bytes, max 256 bytes
		_, err = rand.Read(value)
		assert.NoError(t, err)
		if err = testTrie.TryUpdate(key, value); err != nil {
			t.Fatal("error updating trie", err)
		}

		keys = append(keys, key)
		values = append(values, value)
	}

	// Commit the root to [trieDB]
	root, _, err := testTrie.Commit(nil)
	assert.NoError(t, err)
	err = trieDB.Commit(root, false, nil)
	assert.NoError(t, err)

	return root, keys, values
}
