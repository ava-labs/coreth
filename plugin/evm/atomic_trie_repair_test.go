// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAtomicTrieRepair(t *testing.T) {
	require := require.New(t)
	commitInterval := uint64(4096)

	// create an unrepaired atomic trie
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, Codec, 0, nil, nil, nil)
	require.NoError(err)
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, 0, common.Hash{}, commitInterval)
	require.NoError(err)
	a := atomicBackend.AtomicTrie().(*atomicTrie)

	// make a commit at a height larger than all bonus blocks
	maxBonusBlockHeight := canonicalBlockMainnetHeights[len(canonicalBlockMainnetHeights)-1]
	commitHeight := nearestCommitHeight(maxBonusBlockHeight, commitInterval) + commitInterval
	err = a.commit(commitHeight, types.EmptyRootHash)
	require.NoError(err)
	require.NoError(db.Commit())

	// recreate the trie with the repair constructor
	atomicBackend, err = NewAtomicBackend(
		db, testSharedMemory(), bonusBlockMainnetHeights,
		repo, commitHeight, common.Hash{}, commitInterval,
	)
	require.NoError(err)
	atomicBackend.Repair(mainnetBonusBlocksRlp)
	a = atomicBackend.AtomicTrie().(*atomicTrie)

	// create a map to track the expected items in the atomic trie.
	// note we serialize the atomic ops to bytes so we can compare nil
	// and empty slices as equal
	expectedKeys := 0
	expected := make(map[uint64]map[ids.ID][]byte)
	for height, rlp := range mainnetBonusBlocksRlp {
		txs, err := extractAtomicTxsFromRlp(rlp, Codec, bonusBlockMainnetHeights[height])
		require.NoError(err)
		if len(txs) == 0 {
			continue
		}

		requests := make(map[ids.ID][]byte)
		ops, err := mergeAtomicOps(txs)
		require.NoError(err)
		for id, op := range ops {
			bytes, err := a.codec.Marshal(codecVersion, op)
			require.NoError(err)
			requests[id] = bytes
			expectedKeys++
		}
		expected[height] = requests
	}

	// check that the trie is now repaired
	db.Abort()
	atomicBackend, err = NewAtomicBackend(
		db, testSharedMemory(), bonusBlockMainnetHeights,
		repo, commitHeight, common.Hash{}, commitInterval,
	)
	require.NoError(err)
	a = atomicBackend.AtomicTrie().(*atomicTrie)
	heightsRepaired, err := a.repairAtomicTrie(bonusBlockMainnetHeights, mainnetBonusBlocksRlp)
	require.NoError(err)
	require.Zero(heightsRepaired) // migration should not run a second time

	// iterate over the trie and check it contains the expected items
	root, err := a.Root(commitHeight)
	require.NoError(err)
	it, err := a.Iterator(root, nil)
	require.NoError(err)

	foundKeys := 0
	for it.Next() {
		bytes, err := a.codec.Marshal(codecVersion, it.AtomicOps())
		require.NoError(err)
		require.Equal(expected[it.BlockNumber()][it.BlockchainID()], bytes)
		foundKeys++
	}
	require.Equal(expectedKeys, foundKeys)
}
