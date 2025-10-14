// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/heightindexdb/meterdb"
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
	_ ethdb.Database = (*BlockDatabase)(nil)

	// Key prefixes for block data in the ethdb.
	// REVIEW: I opted to just copy these from libevm since these should never change
	// and we can avoid libevm changes by not needing to export them.
	// Alternatively, we can update libevm to export these or create reader functions.
	chainDBHeaderPrefix    = []byte("h")
	chainDBBlockBodyPrefix = []byte("b")
	chainDBReceiptsPrefix  = []byte("r")

	// Database prefix for storing block database migrator's internal state and progress tracking
	migratorDBPrefix = []byte("migrator")

	// Key for storing the minimum height configuration of the block databases.
	// This value determines the lowest block height that will be stored in the
	// height-indexed block databases. Once set during initialization, the block
	// database min height cannot be changed without recreating the databases.
	blockDBMinHeightKey = []byte("blockdb_min_height")
)

const (
	// Number of elements in the RLP encoded block data
	blockDataElements = 3
	// Number of elements in the RLP encoded receipt data
	receiptDataElements = 2
	blockNumberSize     = 8
	blockHashSize       = 32
)

// BlockDatabase is a wrapper around an ethdb.Database that stores block header
// body, and receipts in separate height-indexed block databases.
// All other keys are stored in the underlying ethdb.Database (chainDB).
type BlockDatabase struct {
	ethdb.Database
	// DB for storing block database state data (ie min height)
	stateDB    database.Database
	blockDB    database.HeightIndex
	receiptsDB database.HeightIndex

	config      blockdb.DatabaseConfig
	blockDBPath string
	minHeight   uint64

	bCache   *blockCache // cache for block header and body
	migrator *blockDatabaseMigrator

	reg prometheus.Registerer
	// todo: use logger for blockdb instances
	//nolint:unused
	logger logging.Logger

	initialized bool
}

type blockDatabaseBatch struct {
	database *BlockDatabase
	ethdb.Batch
}

func NewBlockDatabase(
	stateDB database.Database,
	chainDB ethdb.Database,
	config blockdb.DatabaseConfig,
	blockDBPath string,
	logger logging.Logger,
	reg prometheus.Registerer,
) *BlockDatabase {
	return &BlockDatabase{
		stateDB:     stateDB,
		Database:    chainDB,
		bCache:      newBlockCache(),
		blockDBPath: blockDBPath,
		config:      config,
		reg:         reg,
		logger:      logger,
	}
}

// InitWithStateSync initializes the height-indexed databases with the
// appropriate minimum height based on existing configuration and state sync settings.
//
// Initialization cases (in order of precedence):
//  1. Databases already exist → loads with existing min height
//  2. Data to migrate exists → initializes with min block height to migrate
//  3. No data to migrate + state sync enabled → defers initialization
//  4. No data to migrate + state sync disabled → initializes with min height 1
//
// Returns true if databases were initialized, false otherwise.
func (db *BlockDatabase) InitWithStateSync(stateSyncEnabled bool) (bool, error) {
	minHeight, err := getDatabaseMinHeight(db.stateDB)
	if err != nil {
		return false, err
	}

	// Databases already exist, load with existing min height
	if minHeight != nil {
		if err := db.InitWithMinHeight(*minHeight); err != nil {
			return false, err
		}
		return true, nil
	}

	// Data to migrate exists, initialize with min block height to migrate
	minMigrateHeight := minBlockHeightToMigrate(db.Database)
	if minMigrateHeight != nil {
		if err := db.InitWithMinHeight(*minMigrateHeight); err != nil {
			return false, err
		}
		return true, nil
	}

	// No data to migrate and state sync disabled, initialize with min height 1
	if !stateSyncEnabled {
		// Genesis block is not stored in height-indexed databases to avoid
		// min height complexity with different node types (pruned vs archive).
		if err := db.InitWithMinHeight(1); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// InitWithMinHeight initializes the height-indexed databases with the provided minimum height.
func (db *BlockDatabase) InitWithMinHeight(minHeight uint64) error {
	if db.initialized {
		log.Warn("InitWithMinHeight called on a block database that is already initialized")
		return nil
	}
	log.Info("Initializing block database with min height", "minHeight", minHeight)

	if err := db.stateDB.Put(blockDBMinHeightKey, encodeBlockNumber(minHeight)); err != nil {
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

	if err := db.initMigrator(); err != nil {
		return fmt.Errorf("failed to init migrator: %w", err)
	}

	db.initialized = true
	db.minHeight = minHeight
	return nil
}

// Migrate migrates block headers, bodies, and receipts from ethDB to the block databases.
func (db *BlockDatabase) Migrate() error {
	if !db.initialized {
		return errors.New("block database must be initialized before migrating")
	}
	return db.migrator.Migrate()
}

func (db *BlockDatabase) Put(key []byte, value []byte) error {
	if !db.shouldWriteToBlockDatabase(key) {
		return db.Database.Put(key, value)
	}

	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	if isReceiptKey(key) {
		return db.writeReceipts(blockNumber, blockHash, value)
	}

	db.bCache.save(key, blockHash, value)
	if bodyData, headerData, ok := db.bCache.get(blockHash); ok {
		db.bCache.clear(blockHash)
		return db.writeBlock(blockNumber, blockHash, headerData, bodyData)
	}
	return nil
}

func (db *BlockDatabase) Get(key []byte) ([]byte, error) {
	if !db.shouldWriteToBlockDatabase(key) {
		return db.Database.Get(key)
	}
	blockNumber, blockHash := blockNumberAndHashFromKey(key)

	if isReceiptKey(key) {
		return db.getReceipts(key, blockNumber, blockHash)
	}
	return db.getBlock(key, blockNumber, blockHash)
}

func (db *BlockDatabase) Has(key []byte) (bool, error) {
	if !db.shouldWriteToBlockDatabase(key) {
		return db.Database.Has(key)
	}
	data, err := db.Get(key)
	if err != nil {
		return false, err
	}
	return data != nil, nil
}

func (db *BlockDatabase) Delete(key []byte) error {
	if !db.shouldWriteToBlockDatabase(key) {
		return db.Database.Delete(key)
	}

	// no-op since deleting written blocks is not supported
	return nil
}

func (db *BlockDatabase) Close() error {
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

func (db *BlockDatabase) initMigrator() error {
	if db.migrator != nil {
		return nil
	}
	migratorStateDB := prefixdb.New(migratorDBPrefix, db.stateDB)
	migrator, err := NewBlockDatabaseMigrator(migratorStateDB, db.blockDB, db.receiptsDB, db.Database)
	if err != nil {
		return err
	}
	db.migrator = migrator
	return nil
}

func (db *BlockDatabase) createMeteredBlockDatabase(namespace string, minHeight uint64) (database.HeightIndex, error) {
	path := filepath.Join(db.blockDBPath, namespace)
	config := db.config.WithDir(path).WithMinimumHeight(minHeight)
	newDB, err := blockdb.New(config, db.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create block database at %s: %w", path, err)
	}

	meteredDB, err := meterdb.New(db.reg, namespace, newDB)
	if err != nil {
		if err := newDB.Close(); err != nil {
			return nil, fmt.Errorf("failed to close block database: %w", err)
		}
		return nil, fmt.Errorf("failed to create metered %s database: %w", namespace, err)
	}

	return meteredDB, nil
}

func (db *BlockDatabase) getReceipts(key []byte, blockNumber uint64, blockHash common.Hash) ([]byte, error) {
	encodedReceiptData, err := db.receiptsDB.Get(blockNumber)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) && db.isMigrationNeeded() {
			return db.Database.Get(key)
		}
		return nil, err
	}

	decodedHash, receiptData, err := decodeReceiptData(encodedReceiptData)
	if err != nil {
		return nil, err
	}
	if decodedHash != blockHash {
		return nil, nil
	}

	return receiptData, nil
}

func (db *BlockDatabase) getBlock(key []byte, blockNumber uint64, blockHash common.Hash) ([]byte, error) {
	encodedBlock, err := db.blockDB.Get(blockNumber)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) && db.isMigrationNeeded() {
			return db.Database.Get(key)
		}
		return nil, err
	}

	decodedHash, header, body, err := decodeBlockData(encodedBlock)
	if err != nil {
		return nil, err
	}
	if decodedHash != blockHash {
		return nil, nil
	}

	if isBodyKey(key) {
		return body, nil
	}
	if isHeaderKey(key) {
		return header, nil
	}

	return nil, fmt.Errorf("unexpected key for block data: %s", key)
}

func (db *BlockDatabase) isMigrationNeeded() bool {
	return db.migrator != nil && db.migrator.Status() != migrationCompleted
}

func (db *BlockDatabase) writeReceipts(blockNumber uint64, blockHash common.Hash, data []byte) error {
	encodedReceipts, err := encodeReceiptData(data, blockHash)
	if err != nil {
		return err
	}
	return db.receiptsDB.Put(blockNumber, encodedReceipts)
}

func (db *BlockDatabase) writeBlock(blockNumber uint64, blockHash common.Hash, header []byte, body []byte) error {
	encodedBlock, err := encodeBlockData(header, body, blockHash)
	if err != nil {
		return err
	}
	return db.blockDB.Put(blockNumber, encodedBlock)
}

func (db *BlockDatabase) shouldWriteToBlockDatabase(key []byte) bool {
	if !db.initialized {
		return false
	}

	isTargetKey := isBodyKey(key) || isHeaderKey(key) || isReceiptKey(key)
	if !isTargetKey {
		return false
	}

	// Only blocks with height >= min height are stored in block database
	if blockNumber, _ := blockNumberAndHashFromKey(key); blockNumber < db.minHeight {
		return false
	}

	return true
}

func (db *BlockDatabase) NewBatch() ethdb.Batch {
	return blockDatabaseBatch{
		database: db,
		Batch:    db.Database.NewBatch(),
	}
}

func (db *BlockDatabase) NewBatchWithSize(size int) ethdb.Batch {
	return blockDatabaseBatch{
		database: db,
		Batch:    db.Database.NewBatchWithSize(size),
	}
}

func (batch blockDatabaseBatch) Put(key []byte, value []byte) error {
	if !batch.database.shouldWriteToBlockDatabase(key) {
		return batch.Batch.Put(key, value)
	}
	return batch.database.Put(key, value)
}

func (batch blockDatabaseBatch) Delete(key []byte) error {
	if !batch.database.shouldWriteToBlockDatabase(key) {
		return batch.Batch.Delete(key)
	}
	return batch.database.Delete(key)
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
	blockHash := common.BytesToHash(key[prefixLen+blockNumberSize:])
	return blockNumber, blockHash
}

func decodeBlockData(encodedBlock []byte) (hash common.Hash, header []byte, body []byte, err error) {
	var blockData [][]byte
	if err := rlp.DecodeBytes(encodedBlock, &blockData); err != nil {
		return common.Hash{}, nil, nil, err
	}

	if len(blockData) != blockDataElements {
		return common.Hash{}, nil, nil, fmt.Errorf(
			"invalid block data format: expected %d elements, got %d",
			blockDataElements,
			len(blockData),
		)
	}

	return common.BytesToHash(blockData[0]), blockData[1], blockData[2], nil
}

func encodeBlockData(header []byte, body []byte, blockHash common.Hash) ([]byte, error) {
	return rlp.EncodeToBytes([][]byte{blockHash.Bytes(), header, body})
}

func encodeReceiptData(receipts []byte, blockHash common.Hash) ([]byte, error) {
	return rlp.EncodeToBytes([][]byte{blockHash.Bytes(), receipts})
}

func decodeReceiptData(encodedReceipts []byte) (common.Hash, []byte, error) {
	var receiptData [][]byte
	if err := rlp.DecodeBytes(encodedReceipts, &receiptData); err != nil {
		return common.Hash{}, nil, err
	}

	if len(receiptData) != receiptDataElements {
		return common.Hash{}, nil, fmt.Errorf(
			"invalid receipt data format: expected %d elements, got %d",
			receiptDataElements,
			len(receiptData),
		)
	}

	return common.BytesToHash(receiptData[0]), receiptData[1], nil
}

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, blockNumberSize)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func isBodyKey(key []byte) bool {
	if len(key) != len(chainDBBlockBodyPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, chainDBBlockBodyPrefix)
}

func isHeaderKey(key []byte) bool {
	if len(key) != len(chainDBHeaderPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, chainDBHeaderPrefix)
}

func isReceiptKey(key []byte) bool {
	if len(key) != len(chainDBReceiptsPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, chainDBReceiptsPrefix)
}

func getDatabaseMinHeight(db database.Database) (*uint64, error) {
	has, err := db.Has(blockDBMinHeightKey)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	minHeightBytes, err := db.Get(blockDBMinHeightKey)
	if err != nil {
		return nil, err
	}
	minHeight := binary.BigEndian.Uint64(minHeightBytes)
	return &minHeight, nil
}

// IsBlockDatabaseCreated checks if block database has already been created
func IsBlockDatabaseCreated(db database.Database) (bool, error) {
	has, err := db.Has(blockDBMinHeightKey)
	if err != nil {
		return false, err
	}
	return has, nil
}
