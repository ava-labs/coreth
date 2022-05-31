// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ethereum/go-ethereum/common"
)

func NewStorageSnapshotIterator(db ethdb.Iteratee, account common.Hash, start, end []byte) ethdb.Iterator {
	prefix := append([]byte(nil), rawdb.SnapshotStoragePrefix...)
	prefix = append(prefix, account[:]...)
	return rawdb.NewPrefixIterator(db, prefix, start, end)
}

func NewAccountSnapshotIterator(db ethdb.Iteratee, start, end []byte) ethdb.Iterator {
	return rawdb.NewValueWrapperIterator(
		rawdb.NewPrefixIterator(db, rawdb.SnapshotAccountPrefix, start, end),
		FullAccountRLP,
	)
}
