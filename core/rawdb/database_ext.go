// (c) 2019-2025, Ava Labs, Inc.
package rawdb

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

// ClearPrefix removes all keys in db that begin with prefix and match an
// expected key length. [keyLen] should include the length of the prefix.
func ClearPrefix(db ethdb.KeyValueStore, prefix []byte, keyLen int) error {
	it := db.NewIterator(prefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		key := common.CopyBytes(it.Key())
		if len(key) != keyLen {
			// avoid deleting keys that do not match the expected length
			continue
		}
		if err := batch.Delete(key); err != nil {
			return err
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	options := []ethrawdb.InspectDatabaseOption{
		ethrawdb.WithInspectDatabaseExtraMeta(snapshotBlockHashKey),
		ethrawdb.WithInspectDatabaseExtraMeta(syncRootKey),
		ethrawdb.WithInspectDatabaseRemoval("Key-Value store", "Difficulties"),
		ethrawdb.WithInspectDatabaseRemoval("Key-Value store", "Beacon sync headers"),
		ethrawdb.WithInspectDatabaseExtraStat(
			"State sync", "Trie segments", func(key []byte) bool {
				return bytes.HasPrefix(key, syncSegmentsPrefix) && len(key) == syncSegmentsKeyLength
			},
		),
		ethrawdb.WithInspectDatabaseExtraStat(
			"State sync", "Storage tries to fetch", func(key []byte) bool {
				return bytes.HasPrefix(key, syncStorageTriesPrefix) && len(key) == syncStorageTriesKeyLength
			},
		),
		ethrawdb.WithInspectDatabaseExtraStat(
			"State sync", "Code to fetch", func(key []byte) bool {
				return bytes.HasPrefix(key, CodeToFetchPrefix) && len(key) == codeToFetchKeyLength
			},
		),
		ethrawdb.WithInspectDatabaseExtraStat(
			"State sync", "Block numbers synced to", func(key []byte) bool {
				return bytes.HasPrefix(key, syncPerformedPrefix) && len(key) == syncPerformedKeyLength
			},
		),
	}

	return ethrawdb.InspectDatabase(db, keyPrefix, keyStart, options...)
}
