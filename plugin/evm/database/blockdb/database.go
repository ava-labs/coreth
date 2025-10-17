// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

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
	// Number of elements in the RLP encoded receipt/header/body data (hash + data)
	hashDataElements = 2
	blockNumberSize  = 8
	blockHashSize    = 32
)

// BlockDatabase is a wrapper around an ethdb.Database that stores block header
// body, and receipts in separate height-indexed block databases.
// All other keys are stored in the underlying ethdb.Database (chainDB).
type BlockDatabase struct {
	ethdb.Database
	// DB for storing block database state data (ie min height)
	stateDB    database.Database
	headerDB   database.HeightIndex
	bodyDB     database.HeightIndex
	receiptsDB database.HeightIndex

	config      blockdb.DatabaseConfig
	blockDBPath string
	minHeight   uint64

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
	headerDB, err := db.createMeteredBlockDatabase("headerdb", minHeight)
	if err != nil {
		return err
	}
	bodyDB, err := db.createMeteredBlockDatabase("bodydb", minHeight)
	if err != nil {
		return err
	}
	receiptsDB, err := db.createMeteredBlockDatabase("receiptsdb", minHeight)
	if err != nil {
		return err
	}
	db.headerDB = headerDB
	db.bodyDB = bodyDB
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
		return writeHashAndData(db.receiptsDB, blockNumber, blockHash, value)
	}
	if isHeaderKey(key) {
		return writeHashAndData(db.headerDB, blockNumber, blockHash, value)
	}
	if isBodyKey(key) {
		return writeHashAndData(db.bodyDB, blockNumber, blockHash, value)
	}
	return nil
}

func (db *BlockDatabase) Get(key []byte) ([]byte, error) {
	if !db.shouldWriteToBlockDatabase(key) {
		return db.Database.Get(key)
	}

	blockNumber, blockHash := blockNumberAndHashFromKey(key)
	var heightDB database.HeightIndex
	switch {
	case isReceiptKey(key):
		heightDB = db.receiptsDB
	case isHeaderKey(key):
		heightDB = db.headerDB
	case isBodyKey(key):
		heightDB = db.bodyDB
	default:
		return nil, fmt.Errorf("unexpected key: %x", key)
	}

	return readHashAndData(heightDB, db.Database, key, blockNumber, blockHash, db.migrator)
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
	if db.headerDB != nil {
		if err := db.headerDB.Close(); err != nil {
			return err
		}
	}
	if db.bodyDB != nil {
		if err := db.bodyDB.Close(); err != nil {
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
	migrator, err := NewBlockDatabaseMigrator(migratorStateDB, db.headerDB, db.bodyDB, db.receiptsDB, db.Database)
	if err != nil {
		return err
	}
	db.migrator = migrator
	return nil
}

func (db *BlockDatabase) createMeteredBlockDatabase(namespace string, minHeight uint64) (database.HeightIndex, error) {
	path := filepath.Join(db.blockDBPath, namespace)
	config := db.config.WithDir(path).WithMinimumHeight(minHeight)
	newDB, err := blockdb.New(config, logging.NoLog{})
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

func (db *BlockDatabase) shouldWriteToBlockDatabase(key []byte) bool {
	if !db.initialized {
		return false
	}

	var prefixLen int
	switch {
	case isBodyKey(key):
		prefixLen = len(chainDBBlockBodyPrefix)
	case isHeaderKey(key):
		prefixLen = len(chainDBHeaderPrefix)
	case isReceiptKey(key):
		prefixLen = len(chainDBReceiptsPrefix)
	default:
		return false
	}
	blockNumber := binary.BigEndian.Uint64(key[prefixLen : prefixLen+8])
	return blockNumber >= db.minHeight
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

// writeHashAndData encodes [hash, data] and writes to the provided height-index db at the given height.
func writeHashAndData(db database.HeightIndex, height uint64, blockHash common.Hash, data []byte) error {
	encoded, err := rlp.EncodeToBytes([][]byte{blockHash.Bytes(), data})
	if err != nil {
		return err
	}
	return db.Put(height, encoded)
}

// readHashAndData reads data from the height-indexed database and falls back
// to the chain database if the data is not found and migration is still needed.
func readHashAndData(
	heightDB database.HeightIndex,
	chainDB ethdb.Database,
	key []byte,
	blockNumber uint64,
	blockHash common.Hash,
	migrator *blockDatabaseMigrator,
) ([]byte, error) {
	encodedData, err := heightDB.Get(blockNumber)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) && migrator != nil && migrator.Status() != migrationCompleted {
			return chainDB.Get(key)
		}
		return nil, err
	}

	var elems [][]byte
	if err := rlp.DecodeBytes(encodedData, &elems); err != nil {
		return nil, err
	}
	if len(elems) != hashDataElements {
		return nil, fmt.Errorf(
			"invalid hash+data format: expected %d elements, got %d",
			hashDataElements,
			len(elems),
		)
	}
	decodedHash := common.BytesToHash(elems[0])
	if decodedHash != blockHash {
		return nil, nil
	}

	return elems[1], nil
}

// IsBlockDatabaseCreated checks if block databases have already been created
func IsBlockDatabaseCreated(db database.Database) (bool, error) {
	has, err := db.Has(blockDBMinHeightKey)
	if err != nil {
		return false, err
	}
	return has, nil
}
