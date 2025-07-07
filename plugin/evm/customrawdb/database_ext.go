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
func ParseStateSchemeExt(provided string, disk ethdb.Database) (string, error) {
	// Check for custom scheme
	if provided == FirewoodScheme {
		if diskScheme := rawdb.ReadStateScheme(disk); diskScheme != "" {
			// Valid scheme on disk mismatched
			return "", fmt.Errorf("State scheme %s already set on disk, can't use Firewood", diskScheme)
		}
		// If no conflicting scheme is found, is valid.
		return FirewoodScheme, nil
	}

	// Check for valid eth scheme
	return rawdb.ParseStateScheme(provided, disk)
}

// WriteChainDataPath writes the path to the database files.
// This should only be written once.
func WriteChainDataPath(db ethdb.KeyValueWriter, path string) error {
	// Check if the path is valid
	if path == "" {
		return fmt.Errorf("Invalid state path %q", path)
	}

	// Write the state path to the database
	return db.Put(chainDataPathKey, []byte(path))
}

// ReadChainDataPath reads the path to the database files.
func ReadChainDataPath(db ethdb.KeyValueReader) (string, error) {
	has, err := db.Has(chainDataPathKey)
	if err != nil || !has {
		return "", err
	}
	// Read the state path from the database
	path, err := db.Get(chainDataPathKey)
	if err != nil {
		return "", err
	}

	return string(path), nil
}

// WriteFirewoodGenesisRoot writes the genesis root to the database.
func WriteFirewoodGenesisRoot(db ethdb.KeyValueWriter, root common.Hash) error {
	// Write the genesis root to the database
	return db.Put(firewoodGenesisRootKey, root[:])
}

// ReadFirewoodGenesisRoot reads the genesis root from the database.
func ReadFirewoodGenesisRoot(db ethdb.KeyValueReader) (common.Hash, error) {
	has, err := db.Has(firewoodGenesisRootKey)
	if err != nil || !has {
		return common.Hash{}, err
	}
	// Read the genesis root from the database
	rootBytes, err := db.Get(firewoodGenesisRootKey)
	if err != nil {
		return common.Hash{}, err
	}
	if len(rootBytes) != common.HashLength {
		return common.Hash{}, fmt.Errorf("Invalid genesis root length %d", len(rootBytes))
	}

	return common.BytesToHash(rootBytes), nil
}
