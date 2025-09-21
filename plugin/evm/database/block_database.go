// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
)

var (
	_ ethdb.Database = (*wrappedBlockDatabase)(nil)

	// chaindb database scheme prefixes
	// should these be exported from libevm or just keep a copy of the prefix here?
	// these should really never change, so it might be fine to keep a copy here to avoid
	// libevm changes
	chaindbHeaderPrefix    = []byte("h")
	chaindbBlockBodyPrefix = []byte("b")
)

type wrappedBlockDatabase struct {
	blockdb database.BlockDatabase
	ethdb.Database

	bodyCache   sync.Map
	headerCache sync.Map
	migrator    *blockDatabaseMigrator
}

type wrappedBlockDatabaseBatch struct {
	database *wrappedBlockDatabase
	ethdb.Batch
}

func NewWrappedBlockDatabase(db database.Database, blockdb database.BlockDatabase, chaindb ethdb.Database, withMigrator bool) (*wrappedBlockDatabase, error) {
	var migrator *blockDatabaseMigrator
	if withMigrator {
		var err error
		migrator, err = NewBlockDatabaseMigrator(db, blockdb, chaindb)
		if err != nil {
			return nil, err
		}
		if migrator.getStatus() != migrationStatusCompleted {
			if err := migrator.Migrate(); err != nil {
				return nil, err
			}
		}
	}
	return &wrappedBlockDatabase{
		blockdb:     blockdb,
		Database:    chaindb,
		bodyCache:   sync.Map{},
		headerCache: sync.Map{},
		migrator:    migrator,
	}, nil
}

func (db *wrappedBlockDatabase) Put(key []byte, value []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Put(key, value)
	}

	// save block header or body to cache. Only write to database if we have both
	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	if isBodyKey(key) {
		db.bodyCache.Store(blockHash, value)
	} else if isHeaderKey(key) {
		db.headerCache.Store(blockHash, value)
	}

	// Check if we have both header and body before proceeding
	bodyData, hasBody := db.bodyCache.Load(blockHash)
	headerData, hasHeader := db.headerCache.Load(blockHash)

	// If we don't have both, just return
	if !hasBody || !hasHeader {
		return nil
	}

	// We have both, encode block data as RLP array before writing to blockdb
	encodedBlock, err := encodeBlockData(headerData.([]byte), bodyData.([]byte), blockHash)
	if err != nil {
		return err
	}
	err = db.blockdb.WriteBlock(blockNumber, encodedBlock)
	if err != nil {
		return err
	}

	return nil
}

func (db *wrappedBlockDatabase) Delete(key []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Delete(key)
	}
	// no-op since blockdb does not support deleting written blocks
	return nil
}

func (db *wrappedBlockDatabase) Has(key []byte) (bool, error) {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Has(key)
	}
	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	encodedBlock, err := db.blockdb.ReadBlock(blockNumber)
	if err != nil {
		if errors.Is(err, blockdb.ErrBlockNotFound) {
			if db.migrator != nil && db.migrator.getStatus() != migrationStatusCompleted {
				return db.Database.Has(key)
			}
		}
		return false, err
	}

	// Decode and validate the hash
	decodedHash, _, _, err := decodeBlockData(encodedBlock)
	if err != nil {
		return false, err
	}
	if decodedHash != blockHash {
		return false, nil
	}

	return true, nil
}

func (db *wrappedBlockDatabase) Get(key []byte) ([]byte, error) {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Get(key)
	}
	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	// read block from blockdb
	encodedBlock, err := db.blockdb.ReadBlock(blockNumber)
	if err != nil {
		// fallback to ethdb during migration if block is not found
		if errors.Is(err, blockdb.ErrBlockNotFound) {
			if db.migrator != nil && db.migrator.getStatus() != migrationStatusCompleted {
				return db.Database.Get(key)
			}
		}
		return nil, err
	}

	return extractBlockData(encodedBlock, blockHash, key), nil
}

func (db *wrappedBlockDatabase) NewBatch() ethdb.Batch {
	return wrappedBlockDatabaseBatch{
		database: db,
		Batch:    db.Database.NewBatch(),
	}
}

func (db *wrappedBlockDatabase) Close() error {
	if db.migrator != nil {
		db.migrator.Stop()
	}
	err := db.blockdb.Close()
	if err != nil {
		return err
	}
	return db.Database.Close()
}

func (batch wrappedBlockDatabaseBatch) Put(key []byte, value []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return batch.Batch.Put(key, value)
	}
	return batch.database.Put(key, value)
}

func (batch wrappedBlockDatabaseBatch) Delete(key []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return batch.Batch.Delete(key)
	}
	return batch.database.Delete(key)
}

// isBlockBodyOrHeaderKey checks if the key is the key rawdb uses to
// store rlp encoded block header and body.
func isBlockBodyOrHeaderKey(key []byte) bool {
	return isBodyKey(key) || isHeaderKey(key)
}

func blockNumberAndHashFromKey(key []byte) (uint64, common.Hash) {
	var prefixLen int
	if isBodyKey(key) {
		prefixLen = len(chaindbBlockBodyPrefix)
	} else if isHeaderKey(key) {
		prefixLen = len(chaindbHeaderPrefix)
	}
	blockNumber := binary.BigEndian.Uint64(key[prefixLen : prefixLen+8])
	blockHash := common.BytesToHash(key[prefixLen+8:])
	return blockNumber, blockHash
}

// decodeBlockData decodes the RLP-encoded block data back into hash, header and body
func decodeBlockData(encodedBlock []byte) (hash common.Hash, header []byte, body []byte, err error) {
	var blockData [][]byte
	if err := rlp.DecodeBytes(encodedBlock, &blockData); err != nil {
		return common.Hash{}, nil, nil, err
	}

	if len(blockData) != 3 {
		return common.Hash{}, nil, nil, fmt.Errorf("invalid block data format: expected 3 elements, got %d", len(blockData))
	}

	hash = common.BytesToHash(blockData[0])
	header = blockData[1]
	body = blockData[2]

	return hash, header, body, nil
}

func encodeBlockData(header []byte, body []byte, blockHash common.Hash) ([]byte, error) {
	return rlp.EncodeToBytes([][]byte{blockHash.Bytes(), header, body})
}

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func isBodyKey(key []byte) bool {
	if len(key) != len(chaindbBlockBodyPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chaindbBlockBodyPrefix)
}

func isHeaderKey(key []byte) bool {
	if len(key) != len(chaindbHeaderPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chaindbHeaderPrefix)
}

// IsBlockDBUsed checks if block database has been enabled before.
func IsBlockDBUsed(db database.Database) bool {
	has, err := db.Has(migrationStatusKey)
	if err != nil {
		return false
	}
	return has
}

// extractBlockData decodes the encoded block data, validates the hash, and returns the appropriate data based on the key
func extractBlockData(encodedBlock []byte, blockHash common.Hash, key []byte) []byte {
	decodedHash, header, body, err := decodeBlockData(encodedBlock)
	if err != nil {
		return nil
	}
	if decodedHash != blockHash {
		return nil
	}

	var data []byte
	if isBodyKey(key) {
		data = body
	} else if isHeaderKey(key) {
		data = header
	}
	return data
}
