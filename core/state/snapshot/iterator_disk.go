// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ethereum/go-ethereum/common"
)

// NewStorageSnapshotIterator returns an iterator over storage slots of [account].
// Note: Removes storage prefixes and the account hash from keys before returning them.
func NewStorageSnapshotIterator(db ethdb.Iteratee, account common.Hash, start, end []byte) ethdb.Iterator {
	prefix := append([]byte(nil), rawdb.SnapshotStoragePrefix...)
	prefix = append(prefix, account[:]...)
	return rawdb.NewPrefixIterator(db, prefix, common.HashLength, start, end)
}

// NewAccountSnapshotIterator returns an iterator over accounts in the snapshot.
// Values are returned in consensus format (FullAccountRLP) for compatibility with
// values stored in the trie.
func NewAccountSnapshotIterator(db ethdb.Iteratee, start, end []byte) ethdb.Iterator {
	return rawdb.NewValueWrapperIterator(
		rawdb.NewPrefixIterator(db, rawdb.SnapshotAccountPrefix, common.HashLength, start, end),
		FullAccountRLP,
	)
}
