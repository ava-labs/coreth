// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
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
	atomicBackend, err = NewAtomicBackendWithBonusBlockRepair(
		db, testSharedMemory(), bonusBlockMainnetHeights, mainnetBonusBlocksRlp,
		repo, commitHeight, common.Hash{}, commitInterval,
	)
	require.NoError(err)
	a = atomicBackend.AtomicTrie().(*atomicTrie)

	numRepairHeights := 0
	for height, rlp := range mainnetBonusBlocksRlp {
		txs, err := extractAtomicTxsFromRlp(rlp, Codec, bonusBlockMainnetHeights[height])
		require.NoError(err)
		if len(txs) > 0 {
			numRepairHeights++
		}
	}
	require.Equal(numRepairHeights, a.heightsRepaired)

	// check that the trie is now repaired
	db.Abort()
	atomicBackend, err = NewAtomicBackendWithBonusBlockRepair(
		db, testSharedMemory(), bonusBlockMainnetHeights, mainnetBonusBlocksRlp,
		repo, 0, common.Hash{}, commitInterval,
	)
	require.NoError(err)
	a = atomicBackend.AtomicTrie().(*atomicTrie)
	err = a.repairAtomicTrie(bonusBlockMainnetHeights, mainnetBonusBlocksRlp)
	require.NoError(err)
}
