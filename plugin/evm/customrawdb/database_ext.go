// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	type stat = rawdb.DatabaseStat
	stats := []struct {
		name      string
		keyLen    int
		keyPrefix []byte
		stat      *stat
	}{
		{"Trie segments", syncSegmentsKeyLength, syncSegmentsPrefix, &stat{}},
		{"Storage tries to fetch", syncStorageTriesKeyLength, syncStorageTriesPrefix, &stat{}},
		{"Code to fetch", codeToFetchKeyLength, CodeToFetchPrefix, &stat{}},
		{"Block numbers synced to", syncPerformedKeyLength, syncPerformedPrefix, &stat{}},
	}

	options := []rawdb.InspectDatabaseOption{
		rawdb.WithDatabaseMetadataKeys(func(key []byte) bool {
			return bytes.Equal(key, snapshotBlockHashKey) ||
				bytes.Equal(key, syncRootKey)
		}),
		rawdb.WithDatabaseStatRecorder(func(key []byte, size common.StorageSize) bool {
			for _, s := range stats {
				if len(key) == s.keyLen && bytes.HasPrefix(key, s.keyPrefix) {
					s.stat.Add(size)
					return true
				}
			}
			return false
		}),
		rawdb.WithDatabaseStatsTransformer(func(rows [][]string) [][]string {
			newRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				switch db, cat := row[0], row[1]; {
				// Discard rows specific to libevm (geth) but irrelevant to coreth.
				case db == "Key-Value store" && (cat == "Difficulties" || cat == "Beacon sync headers"):
				case db == "Ancient store (Chain)":
				default:
					newRows = append(newRows, row)
				}
			}
			for _, s := range stats {
				newRows = append(newRows, []string{"State sync", s.name, s.stat.Size(), s.stat.Count()})
			}
			return newRows
		}),
	}

	return rawdb.InspectDatabase(db, keyPrefix, keyStart, options...)
}

// ParseStateSchemeExt parses the state scheme from the provided string.
// It checks the rawdb package for HashDB or PathDB schemes.
// If it's neither of those, it checks to see if the string is set to FirewoodScheme.
func ParseStateSchemeExt(provided string, disk ethdb.Database) (string, error) {
	// Check for custom scheme
	if provided == FirewoodScheme {
		// TODO: Check if the database is a firewood database
		// Assume valid scheme for now
		return FirewoodScheme, nil
	}

	// Check for eth scheme
	scheme, err := rawdb.ParseStateScheme(provided, disk)
	if err == nil {
		// Found valid eth scheme
		return scheme, nil
	} else if rawdb.ReadStateScheme(disk) != "" {
		// Valid scheme on disk mismatched
		return scheme, err
	}

	return "", fmt.Errorf("Unknown state scheme %q", provided)
}

// WriteDatabasePath writes the path to the database files.
// This should only be written once.
func WriteDatabasePath(db ethdb.KeyValueWriter, path string) error {
	// Check if the path is valid
	if path == "" {
		return fmt.Errorf("Invalid state path %q", path)
	}

	// Write the state path to the database
	return db.Put(statePathKey, []byte(path))
}

// ReadDatabasePath reads the path to the database files.
func ReadDatabasePath(db ethdb.KeyValueReader) (string, error) {
	has, err := db.Has(statePathKey)
	if err != nil || !has {
		return "", err
	}
	// Read the state path from the database
	path, err := db.Get(statePathKey)
	if err != nil {
		return "", err
	}

	return string(path), nil
}
