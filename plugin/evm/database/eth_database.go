// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/meterblockdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ ethdb.Database = (*WrappedEthDatabase)(nil)

	// chainDB database scheme prefixes
	// should these be exported from libevm or just keep a copy of the prefix here?
	// these should really never change, so it might be fine to keep a copy here to avoid
	// libevm changes
	chainDBHeaderPrefix    = []byte("h")
	chainDBBlockBodyPrefix = []byte("b")
	chainDBReceiptsPrefix  = []byte("r")

	migratorDBPrefix = []byte("migrator")

	// min height for block databases after they have been created
	blockDBConfigMinHeightKey = []byte("blockdb_config_min_height")
)

type WrappedEthDatabase struct {
	blockDB    database.BlockDatabase
	receiptsDB database.BlockDatabase
	stateDB    database.Database
	ethdb.Database

	bodyCache   map[common.Hash][]byte
	headerCache map[common.Hash][]byte
	minHeight   *uint64
	blockDBPath string
	reg         prometheus.Registerer
	migrator    *ethDatabaseMigrator
	config      blockdb.DatabaseConfig
	initialized bool
	// todo: use logger for blockdb instances
	//nolint:unused
	logger logging.Logger
	mu     sync.RWMutex
}

type wrappedEthDatabaseBatch struct {
	database *WrappedEthDatabase
	ethdb.Batch
}

func (db *WrappedEthDatabase) createMeteredBlockDatabase(namespace string, minHeight uint64) (database.BlockDatabase, error) {
	path := filepath.Join(db.blockDBPath, namespace)
	config := db.config.WithDir(path).WithMinimumHeight(minHeight)
	blockDB, err := blockdb.New(config, logging.NoLog{})
	if err != nil {
		return nil, fmt.Errorf("failed to create block database at %s: %w", path, err)
	}

	meteredDB, err := meterblockdb.New(db.reg, namespace, blockDB)
	if err != nil {
		return nil, fmt.Errorf("failed to create metered %s database: %w", namespace, err)
	}

	return meteredDB, nil
}

func (db *WrappedEthDatabase) IsInitialized() bool {
	return db.initialized
}

// InitWithMinHeight initializes database wrapper with the databases for
// storing block data and receipts. This is not done on construction because
// the block database may be created before the min height is known due to
// state sync.
func (db *WrappedEthDatabase) InitWithMinHeight(minHeight uint64) error {
	if db.initialized {
		log.Warn("InitWithMinHeight called on a block database that is already initialized")
		return nil
	}
	db.initialized = true
	db.minHeight = &minHeight

	// save min height to stateDB
	if err := db.stateDB.Put(blockDBConfigMinHeightKey, encodeBlockNumber(minHeight)); err != nil {
		return err
	}

	blockDB, err := db.createMeteredBlockDatabase("blockdb", minHeight)
	if err != nil {
		return err
	}
	db.blockDB = blockDB
	receiptsDB, err := db.createMeteredBlockDatabase("receiptsdb", minHeight)
	if err != nil {
		return err
	}
	db.receiptsDB = receiptsDB

	// create migrator
	migratorStateDB := prefixdb.New(migratorDBPrefix, db.stateDB)
	migrator, err := NewBlockDatabaseMigrator(migratorStateDB, blockDB, receiptsDB, db.Database)
	if err != nil {
		return err
	}
	db.migrator = migrator

	return nil
}

func getDatabaseMinHeight(db database.Database) (*uint64, error) {
	has, err := db.Has(blockDBConfigMinHeightKey)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	minHeightBytes, err := db.Get(blockDBConfigMinHeightKey)
	if err != nil {
		return nil, err
	}
	minHeight := binary.BigEndian.Uint64(minHeightBytes)
	return &minHeight, nil
}

// NewWrappedEthDatabase creates a wrapped database that stores block data (headers/bodies)
// and receipts in separate databases.
func NewWrappedEthDatabase(
	stateDB database.Database,
	blockDBPath string,
	chainDB ethdb.Database,
	stateSyncEnabled bool,
	config blockdb.DatabaseConfig,
	autoMigrate bool,
	logger logging.Logger,
	reg prometheus.Registerer,
) (*WrappedEthDatabase, error) {
	wrappedDB := &WrappedEthDatabase{
		stateDB:     stateDB,
		Database:    chainDB,
		bodyCache:   make(map[common.Hash][]byte),
		headerCache: make(map[common.Hash][]byte),
		blockDBPath: blockDBPath,
		config:      config,
		reg:         reg,
		logger:      logger,
	}

	minHeight, err := getDatabaseMinHeight(stateDB)
	if err != nil {
		return nil, err
	}

	// initialize databases if it has not been initialized
	if minHeight != nil {
		if err := wrappedDB.InitWithMinHeight(*minHeight); err != nil {
			return nil, err
		}
	} else {
		minHeight := minBlockHeightToMigrate(chainDB)
		if minHeight != nil {
			// if we have data to migrate, initialize with the min stored block height
			if err := wrappedDB.InitWithMinHeight(*minHeight); err != nil {
				return nil, err
			}
		}
		// if state sync is not enabled, we will store blocks starting from genesis
		// if state sync is enabled, we will need to wait to init after state sync
		// is completed using the last accepted height
		if minHeight == nil && !stateSyncEnabled {
			if err := wrappedDB.InitWithMinHeight(1); err != nil {
				return nil, err
			}
		}
	}

	if autoMigrate && wrappedDB.migrator != nil {
		if err := wrappedDB.migrator.Migrate(); err != nil {
			return nil, err
		}
	}

	return wrappedDB, nil
}

func (db *WrappedEthDatabase) Put(key []byte, value []byte) error {
	if !db.shouldUseBlockDB(key) {
		return db.Database.Put(key, value)
	}

	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	if isReceiptKey(key) {
		return db.writeReceipts(blockNumber, blockHash, value)
	}

	db.saveHeaderOrBodyCache(key, blockHash, value)
	if bodyData, headerData, ok := db.getFullBlockData(blockHash); ok {
		return db.writeBlock(blockNumber, blockHash, headerData, bodyData)
	}
	return nil
}

func (db *WrappedEthDatabase) saveHeaderOrBodyCache(key []byte, blockHash common.Hash, value []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if isBodyKey(key) {
		db.bodyCache[blockHash] = value
	} else if isHeaderKey(key) {
		db.headerCache[blockHash] = value
	}
}

func (db *WrappedEthDatabase) getFullBlockData(blockHash common.Hash) ([]byte, []byte, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	bodyData, hasBody := db.bodyCache[blockHash]
	headerData, hasHeader := db.headerCache[blockHash]
	return bodyData, headerData, hasBody && hasHeader
}

func (db *WrappedEthDatabase) Delete(key []byte) error {
	if !db.shouldUseBlockDB(key) {
		return db.Database.Delete(key)
	}

	// no-op since deleting written blocks is not supported
	return nil
}

// shouldFallbackToEthDBDuringMigration return true if fallback to ethdb is
// necessary during migration if the error is due to block not found
func (db *WrappedEthDatabase) shouldFallbackToEthDBDuringMigration(err error) bool {
	// only handle block not found error
	if !errors.Is(err, blockdb.ErrBlockNotFound) {
		return false
	}
	if db.migrator == nil || db.migrator.getStatus() == migrationStatusCompleted {
		return false
	}
	return true
}

func (db *WrappedEthDatabase) Has(key []byte) (bool, error) {
	if !db.shouldUseBlockDB(key) {
		return db.Database.Has(key)
	}
	data, err := db.Get(key)
	if err != nil {
		return false, err
	}
	return data != nil, nil
}

func (db *WrappedEthDatabase) Get(key []byte) ([]byte, error) {
	if !db.shouldUseBlockDB(key) {
		return db.Database.Get(key)
	}
	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	// if min height is never set, this means our block database has never been loaded
	// then we just return nil since we won't have any data anyways.
	if db.minHeight == nil {
		return nil, nil
	}

	if isReceiptKey(key) {
		encodedReceiptData, err := db.receiptsDB.ReadBlock(blockNumber)
		if err != nil {
			if db.shouldFallbackToEthDBDuringMigration(err) {
				return db.Database.Get(key)
			}
			return nil, err
		}

		// Decode and verify the hash
		decodedHash, receiptData, err := decodeReceiptData(encodedReceiptData)
		if err != nil {
			return nil, err
		}
		if decodedHash != blockHash {
			return nil, nil
		}

		return receiptData, nil
	}

	// Handle header and body keys
	encodedBlock, err := db.blockDB.ReadBlock(blockNumber)
	if err != nil {
		if db.shouldFallbackToEthDBDuringMigration(err) {
			return db.Database.Get(key)
		}
		return nil, err
	}

	return extractBlockData(encodedBlock, blockHash, key), nil
}

func (db *WrappedEthDatabase) writeReceipts(blockNumber uint64, blockHash common.Hash, data []byte) error {
	encodedReceipts, err := encodeReceiptData(data, blockHash)
	if err != nil {
		return err
	}
	return db.receiptsDB.WriteBlock(blockNumber, encodedReceipts)
}

func (db *WrappedEthDatabase) writeBlock(blockNumber uint64, blockHash common.Hash, header []byte, body []byte) error {
	encodedBlock, err := encodeBlockData(header, body, blockHash)
	if err != nil {
		return err
	}
	return db.blockDB.WriteBlock(blockNumber, encodedBlock)
}

func (db *WrappedEthDatabase) NewBatch() ethdb.Batch {
	return wrappedEthDatabaseBatch{
		database: db,
		Batch:    db.Database.NewBatch(),
	}
}

func (db *WrappedEthDatabase) Close() error {
	if db.migrator != nil {
		db.migrator.Stop()
	}
	if db.blockDB != nil {
		err := db.blockDB.Close()
		if err != nil {
			return err
		}
	}
	if db.receiptsDB != nil {
		err := db.receiptsDB.Close()
		if err != nil {
			return err
		}
	}
	return db.Database.Close()
}

func (batch wrappedEthDatabaseBatch) Put(key []byte, value []byte) error {
	if !batch.database.shouldUseBlockDB(key) {
		return batch.Batch.Put(key, value)
	}
	return batch.database.Put(key, value)
}

func (batch wrappedEthDatabaseBatch) Delete(key []byte) error {
	if !batch.database.shouldUseBlockDB(key) {
		return batch.Batch.Delete(key)
	}
	return batch.database.Delete(key)
}

// shouldUseBlockDB returns true if the value for the eth database key should
// instead be written to the block database.
func (db *WrappedEthDatabase) shouldUseBlockDB(key []byte) bool {
	if !db.initialized || db.minHeight == nil {
		return false
	}

	isTargetKey := isBodyKey(key) || isHeaderKey(key) || isReceiptKey(key)
	if !isTargetKey {
		return false
	}

	// only blocks with height >= min height are stored in block database
	// genesis blocks is not stored in block database to avoid complicities due to state sync
	if blockNumber, _ := blockNumberAndHashFromKey(key); blockNumber < *db.minHeight {
		return false
	}

	return true
}

func blockNumberAndHashFromKey(key []byte) (uint64, common.Hash) {
	var prefixLen int
	switch {
	case isBodyKey(key):
		prefixLen = len(chainDBBlockBodyPrefix)
	case isHeaderKey(key):
		prefixLen = len(chainDBHeaderPrefix)
	case isReceiptKey(key):
		prefixLen = len(chainDBReceiptsPrefix)
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

func encodeReceiptData(receipts []byte, blockHash common.Hash) ([]byte, error) {
	return rlp.EncodeToBytes([][]byte{blockHash.Bytes(), receipts})
}

func decodeReceiptData(encodedReceipts []byte) (hash common.Hash, receipts []byte, err error) {
	var receiptData [][]byte
	if err := rlp.DecodeBytes(encodedReceipts, &receiptData); err != nil {
		return common.Hash{}, nil, err
	}

	if len(receiptData) != 2 {
		return common.Hash{}, nil, fmt.Errorf("invalid receipt data format: expected 2 elements, got %d", len(receiptData))
	}

	hash = common.BytesToHash(receiptData[0])
	receipts = receiptData[1]
	return hash, receipts, nil
}

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func isBodyKey(key []byte) bool {
	if len(key) != len(chainDBBlockBodyPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chainDBBlockBodyPrefix)
}

func isHeaderKey(key []byte) bool {
	if len(key) != len(chainDBHeaderPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chainDBHeaderPrefix)
}

func isReceiptKey(key []byte) bool {
	if len(key) != len(chainDBReceiptsPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chainDBReceiptsPrefix)
}

// IsBlockDBInitialized checks if block database has been initialized
func IsBlockDBInitialized(db database.Database) bool {
	has, err := db.Has(blockDBConfigMinHeightKey)
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
