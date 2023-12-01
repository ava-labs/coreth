// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicTrieRepairHeightMap(t *testing.T) {
	require := require.New(t)

	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), 0, nil, nil, nil)
	require.NoError(err)
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, 0, common.Hash{}, testCommitInterval)
	require.NoError(err)
	atomicTrie := atomicBackend.AtomicTrie().(*atomicTrie)

	heightMap := make(map[uint64]common.Hash)
	lastAccepted := uint64(testCommitInterval*3 + 5) // process 305 blocks so that we get three commits (100, 200, 300)
	for height := uint64(1); height <= lastAccepted; height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		err := indexAtomicTxs(atomicTrie, height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			root, _ := atomicTrie.LastCommitted()
			heightMap[height] = root
		}
	}
	// ensure we have 3 roots
	require.Len(heightMap, 3)

	// Verify that [atomicTrie] can access each of the expected roots
	verifyRoots := func(expectZero bool) {
		for height, hash := range heightMap {
			root, err := atomicTrie.Root(height)
			require.NoError(err)
			if expectZero {
				require.Zero(root)
			} else {
				require.Equal(hash, root)
			}
		}
	}
	verifyRoots(false)

	// destroy the height map
	for height := range heightMap {
		err := atomicTrie.metadataDB.Delete(database.PackUInt64(height))
		require.NoError(err)
	}
	require.NoError(db.Commit())
	verifyRoots(true)

	// repair the height map
	repaired, err := atomicTrie.repairHeightMap(db, lastAccepted)
	require.NoError(err)
	verifyRoots(false)
	require.True(repaired)

	// partially destroy the height map
	_, lastHeight := atomicTrie.LastCommitted()
	err = atomicTrie.metadataDB.Delete(database.PackUInt64(lastHeight))
	require.NoError(err)
	err = atomicTrie.metadataDB.Put(
		heightMapRepairKey,
		database.PackUInt64(lastHeight-testCommitInterval),
	)
	require.NoError(err)

	// repair the height map
	repaired, err = atomicTrie.repairHeightMap(db, lastAccepted)
	require.NoError(err)
	verifyRoots(false)
	require.True(repaired)

	// try to repair the height map again
	repaired, err = atomicTrie.repairHeightMap(db, lastAccepted)
	require.NoError(err)
	require.False(repaired)
}
