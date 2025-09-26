// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
)

var (
	migrationStatusKey = []byte("migration_status")

	// headBlockNumberKey is used to store the head block number
	headBlockNumberKey = []byte("head_block_number")
)

type migrationStatus int

const (
	migrationStatusNotStarted migrationStatus = iota
	migrationStatusInProgress
	migrationStatusCompleted
)

const (
	logProgressInterval = 5 * time.Minute // Log every 5 minutes
)

type ethDatabaseMigrator struct {
	stateDB         database.Database
	chainDB         ethdb.Database
	blockDB         database.BlockDatabase
	receiptsDB      database.BlockDatabase
	status          migrationStatus
	blocksProcessed int64
	headBlockNumber *uint64
	stopChan        chan struct{}
	stopOnce        sync.Once
	stopped         bool
	mu              sync.RWMutex // protects status field
}

func getMigrationStatus(db database.Database) (migrationStatus, error) {
	var status migrationStatus
	has, err := db.Has(migrationStatusKey)
	if err != nil {
		return status, err
	}
	if !has {
		return migrationStatusNotStarted, nil
	}
	statusBytes, err := db.Get(migrationStatusKey)
	if err != nil {
		return status, err
	}
	return migrationStatus(statusBytes[0]), nil
}

func getHeadBlockNumber(db database.Database) (*uint64, error) {
	has, err := db.Has(headBlockNumberKey)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	blockNumberBytes, err := db.Get(headBlockNumberKey)
	if err != nil {
		return nil, err
	}
	blockNumber := binary.BigEndian.Uint64(blockNumberBytes)
	if blockNumber == 0 {
		return nil, nil
	}
	return &blockNumber, nil
}

// NewBlockDatabaseMigrator creates a new block database migrator
func NewBlockDatabaseMigrator(stateDB database.Database, blockDB database.BlockDatabase, receiptsDB database.BlockDatabase, chainDB ethdb.Database) (*ethDatabaseMigrator, error) {
	mStatus, err := getMigrationStatus(stateDB)
	if err != nil {
		return nil, err
	}

	headBlockNumber, err := getHeadBlockNumber(stateDB)
	if err != nil {
		return nil, err
	}

	return &ethDatabaseMigrator{
		status:          mStatus,
		headBlockNumber: headBlockNumber,
		blockDB:         blockDB,
		receiptsDB:      receiptsDB,
		stateDB:         stateDB,
		chainDB:         chainDB,
		stopChan:        make(chan struct{}),
	}, nil
}

// Migrate starts the migration process
func (m *ethDatabaseMigrator) Migrate() error {
	if m.getStatus() == migrationStatusCompleted {
		return nil
	}
	if m.stopped {
		return errors.New("migration has been stopped and cannot be restarted")
	}

	// Save head block number if starting migration for the first time
	// This will allow us to estimate when the migration will complete
	if m.getStatus() == migrationStatusNotStarted {
		headHash := rawdb.ReadHeadHeaderHash(m.chainDB)
		if headHash != (common.Hash{}) {
			headBlockNumber := rawdb.ReadHeaderNumber(m.chainDB, headHash)
			if headBlockNumber != nil && *headBlockNumber > 0 {
				m.headBlockNumber = headBlockNumber
				if err := m.saveHeadBlockNumber(); err != nil {
					return fmt.Errorf("failed to save head block number: %w", err)
				}
				log.Info("blockdb migration: saved head block number", "head_block_number", *m.headBlockNumber)
			}
		}
	}

	// Initialize migration state
	atomic.StoreInt64(&m.blocksProcessed, 0)
	if err := m.saveMigrationStatus(migrationStatusInProgress); err != nil {
		return err
	}

	// Start migration in background
	go func() {
		if err := m.runMigration(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, database.ErrClosed) {
				return
			}
			log.Error("migration failed", "err", err)
		}
	}()

	return nil
}

// Stop stops the migration process
func (m *ethDatabaseMigrator) Stop() {
	m.stopOnce.Do(func() {
		m.stopped = true
		close(m.stopChan)
	})
}

func (m *ethDatabaseMigrator) runMigration() error {
	var (
		etaTracker    = timer.NewEtaTracker(10, 1.2)
		iter          = m.chainDB.NewIterator(chainDBHeaderPrefix, nil)
		startTime     = time.Now()
		timeOfNextLog = startTime.Add(logProgressInterval)
	)
	defer iter.Release()

	log.Info("blockdb migration started")

	// Add the first sample to the tracker to establish an accurate baseline
	if m.headBlockNumber != nil {
		etaTracker.AddSample(0, *m.headBlockNumber, startTime)
	}

	// iterator will iterate all block headers in ascending order by block number
	for iter.Next() {
		// Check if migration should be stopped
		select {
		case <-m.stopChan:
			log.Info("migration stopped by user", "blocks_processed", atomic.LoadInt64(&m.blocksProcessed))
			return nil
		default:
			// Continue with migration
		}

		key := iter.Key()
		if !shouldMigrateKey(m.chainDB, key) {
			continue
		}

		blockNum, hash := blockNumberAndHashFromKey(key)

		// Migrate block data (header + body)
		if err := m.migrateBlockData(blockNum, hash, iter.Value()); err != nil {
			return fmt.Errorf("failed to migrate block data: %w", err)
		}

		// Migrate receipt data for the same block
		if err := m.migrateReceiptData(blockNum, hash); err != nil {
			return fmt.Errorf("failed to migrate receipt data: %w", err)
		}

		// Batch delete all migrated data from chaindb
		if err := m.cleanupBlockData(blockNum, hash); err != nil {
			return fmt.Errorf("failed to cleanup block data: %w", err)
		}

		blocksProcessed := atomic.AddInt64(&m.blocksProcessed, 1)

		// Log progress every logProgressInterval
		if now := time.Now(); now.After(timeOfNextLog) {
			logFields := []interface{}{
				"blocks_processed", blocksProcessed,
				"last_processed_height", blockNum,
				"time_elapsed", time.Since(startTime),
			}
			if m.headBlockNumber != nil {
				etaPtr, progressPercentage := etaTracker.AddSample(blockNum, *m.headBlockNumber, now)
				if etaPtr != nil {
					logFields = append(logFields, "eta", etaPtr.String())
					logFields = append(logFields, "pctComplete", progressPercentage)
				}
			}

			log.Info("blockdb migration status", logFields...)
			timeOfNextLog = now.Add(logProgressInterval)
		}
	}

	// Migration completed successfully
	if err := m.saveMigrationStatus(migrationStatusCompleted); err != nil {
		log.Error("failed to save completed migration status", "err", err)
	}

	// Log final statistics
	processingTime := time.Since(startTime)
	log.Info("blockdb migration completed",
		"blocks_processed", atomic.LoadInt64(&m.blocksProcessed),
		"total_processing_time", processingTime.String())

	return nil
}

func (m *ethDatabaseMigrator) migrateBlockData(blockNum uint64, hash common.Hash, headerData []byte) error {
	body := rawdb.ReadBody(m.chainDB, hash, blockNum)
	bodyBytes, err := rlp.EncodeToBytes(body)
	if err != nil {
		return fmt.Errorf("failed to encode block body: %w", err)
	}
	encodedBlock, err := encodeBlockData(headerData, bodyBytes, hash)
	if err != nil {
		return fmt.Errorf("failed to encode block data: %w", err)
	}
	if err := m.blockDB.WriteBlock(blockNum, encodedBlock); err != nil {
		return fmt.Errorf("failed to write block to blockDB: %w", err)
	}
	return nil
}

func (m *ethDatabaseMigrator) migrateReceiptData(blockNum uint64, hash common.Hash) error {
	// Read raw receipt bytes directly from chainDB
	receiptBytes := rawdb.ReadReceiptsRLP(m.chainDB, hash, blockNum)
	if receiptBytes == nil {
		// No receipts for this block, skip
		return nil
	}

	// Encode receipts with block hash for verification
	encodedReceipts, err := encodeReceiptData(receiptBytes, hash)
	if err != nil {
		return fmt.Errorf("failed to encode receipts with hash: %w", err)
	}

	// Write to receiptsDB
	if err := m.receiptsDB.WriteBlock(blockNum, encodedReceipts); err != nil {
		return fmt.Errorf("failed to write receipts to receiptsDB: %w", err)
	}

	return nil
}

func (m *ethDatabaseMigrator) cleanupBlockData(blockNum uint64, hash common.Hash) error {
	batch := m.chainDB.NewBatch()

	// Delete header, excluding the header number
	headerKey := append(append(chainDBHeaderPrefix, encodeBlockNumber(blockNum)...), hash.Bytes()...)
	if err := batch.Delete(headerKey); err != nil {
		return fmt.Errorf("failed to delete header from chainDB: %w", err)
	}

	rawdb.DeleteBody(batch, hash, blockNum)
	rawdb.DeleteReceipts(batch, hash, blockNum)

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch to chainDB: %w", err)
	}

	return nil
}

func (m *ethDatabaseMigrator) getStatus() migrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *ethDatabaseMigrator) getBlocksProcessed() uint64 {
	return uint64(atomic.LoadInt64(&m.blocksProcessed))
}

func (m *ethDatabaseMigrator) saveMigrationStatus(status migrationStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status == status {
		return nil
	}
	m.status = status
	return m.stateDB.Put(migrationStatusKey, []byte{byte(status)})
}

func (m *ethDatabaseMigrator) saveHeadBlockNumber() error {
	if m.headBlockNumber == nil {
		return nil
	}
	blockNumberBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(blockNumberBytes, *m.headBlockNumber)
	return m.stateDB.Put(headBlockNumberKey, blockNumberBytes)
}

func shouldMigrateKey(db ethdb.Database, key []byte) bool {
	if !isHeaderKey(key) {
		return false
	}
	blockNum, hash := blockNumberAndHashFromKey(key)

	// genesis blocks should not be stored in block database to avoid complicities due to state sync
	// why? state synced nodes will always have genesis block + blocks from the first state synced block.
	// Because genesis block starts from 0, our index file will always start from 0.
	if blockNum == 0 {
		return false
	}

	canonicalHash := rawdb.ReadCanonicalHash(db, blockNum)
	return canonicalHash == hash
}

// minBlockHeightToMigrate returns the smallest block number that should be migrated.
func minBlockHeightToMigrate(db ethdb.Database) *uint64 {
	iter := db.NewIterator(chainDBHeaderPrefix, nil)
	defer iter.Release()
	for iter.Next() {
		key := iter.Key()
		if !shouldMigrateKey(db, key) {
			continue
		}

		blockNum, _ := blockNumberAndHashFromKey(key)
		return &blockNum
	}
	return nil
}
