// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"testing"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

// assertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by [clientTrieDB] is present the hash is correct.
func assertDBConsistency(t testing.TB, root common.Hash, serverTrieDB, clientTrieDB *trie.Database) {
	trie.AssertTrieConsistency(t, root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
		accHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(val, &acc); err != nil {
			return err
		}
		// check snapshot consistency
		snapshotVal := rawdb.ReadAccountSnapshot(clientTrieDB.DiskDB(), accHash)
		expectedSnapshotVal := snapshot.SlimAccountRLP(acc.Nonce, acc.Balance, acc.Root, acc.CodeHash, acc.IsMultiCoin)
		assert.Equal(t, expectedSnapshotVal, snapshotVal)

		// check code consistency
		if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash[:]) {
			codeHash := common.BytesToHash(acc.CodeHash)
			code := rawdb.ReadCode(clientTrieDB.DiskDB(), codeHash)
			actualHash := crypto.Keccak256Hash(code)
			assert.NotZero(t, len(code))
			assert.Equal(t, codeHash, actualHash)
		}
		if acc.Root == types.EmptyRootHash {
			return nil
		}
		// check storage trie and storage snapshot consistency
		trie.AssertTrieConsistency(t, acc.Root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
			snapshotVal := rawdb.ReadStorageSnapshot(clientTrieDB.DiskDB(), accHash, common.BytesToHash(key))
			assert.Equal(t, val, snapshotVal)
			return nil
		})
		return nil
	})
}
