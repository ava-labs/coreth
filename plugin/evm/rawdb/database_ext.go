// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rawdb

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	type stat = ethrawdb.DatabaseStat
	stats := []struct {
		name      string
		keyLen    int
		keyPrefix []byte
		stat      ethrawdb.DatabaseStat
	}{
		{"Trie segments", syncSegmentsKeyLength, syncSegmentsPrefix, stat{}},
		{"Storage tries to fetch", syncStorageTriesKeyLength, syncStorageTriesPrefix, stat{}},
		{"Code to fetch", codeToFetchKeyLength, CodeToFetchPrefix, stat{}},
		{"Block numbers synced to", syncPerformedKeyLength, syncPerformedPrefix, stat{}},
	}

	options := []ethrawdb.InspectDatabaseOption{
		ethrawdb.WithDatabaseMetadataKeys(func(key []byte) bool {
			return bytes.Equal(key, snapshotBlockHashKey) ||
				bytes.Equal(key, syncRootKey)
		}),
		ethrawdb.WithDatabaseStatRecorder(func(key []byte, size common.StorageSize) bool {
			for _, s := range stats {
				if len(key) == s.keyLen && bytes.HasPrefix(key, s.keyPrefix) {
					s.stat.Add(size)
					return true
				}
			}
			return false
		}),
		ethrawdb.WithDatabaseStatsTransformer(func(rows [][]string) [][]string {
			newRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				database := row[0]
				category := row[1]
				switch {
				case database == "Key-Value store" && category == "Difficulties",
					database == "Key-Value store" && category == "Beacon sync headers",
					database == "Ancient store (Chain)":
					// Discard rows specific to libevm (geth) but irrelevant to coreth.
					continue
				}
				newRows = append(newRows, row)
			}
			for _, s := range stats {
				newRows = append(newRows, []string{"State sync", s.name, s.stat.Size(), s.stat.Count()})
			}
			return newRows
		}),
	}

	return ethrawdb.InspectDatabase(db, keyPrefix, keyStart, options...)
}
