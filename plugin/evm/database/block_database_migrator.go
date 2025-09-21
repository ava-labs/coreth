// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

import (
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

type blockDatabaseMigrator struct {
	// db is the database for the migrator to store any state
	db              database.Database
	blockdb         database.BlockDatabase
	chaindb         ethdb.Database
	status          migrationStatus
	blocksProcessed int64
	headBlockNumber *uint64
	lastLogTime     time.Time
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
	if len(blockNumberBytes) != 8 {
		return nil, fmt.Errorf("invalid head block number bytes length: %d", len(blockNumberBytes))
	}
	blockNumber := binary.BigEndian.Uint64(blockNumberBytes)
	if blockNumber == 0 {
		return nil, nil
	}
	return &blockNumber, nil
}

// NewBlockDatabaseMigrator creates a new block database migrator
func NewBlockDatabaseMigrator(db database.Database, blockdb database.BlockDatabase, chaindb ethdb.Database) (*blockDatabaseMigrator, error) {
	mStatus, err := getMigrationStatus(db)
	if err != nil {
		return nil, err
	}

	headBlockNumber, err := getHeadBlockNumber(db)
	if err != nil {
		return nil, err
	}

	return &blockDatabaseMigrator{
		blockdb:         blockdb,
		db:              db,
		chaindb:         chaindb,
		status:          mStatus,
		headBlockNumber: headBlockNumber,
		stopChan:        make(chan struct{}),
	}, nil
}

// Migrate starts the migration process
func (m *blockDatabaseMigrator) Migrate() error {
	if m.getStatus() == migrationStatusCompleted {
		return nil
	}
	if m.stopped {
		return errors.New("migration has been stopped and cannot be restarted")
	}

	// Save head block number if starting migration for the first time
	// This will allow us to estimate when the migration will complete
	if m.getStatus() == migrationStatusNotStarted {
		headHash := rawdb.ReadHeadHeaderHash(m.chaindb)
		if headHash != (common.Hash{}) {
			headBlockNumber := rawdb.ReadHeaderNumber(m.chaindb, headHash)
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
			log.Error("migration failed", "err", err)
		}
	}()

	return nil
}

// Stop stops the migration process
func (m *blockDatabaseMigrator) Stop() {
	m.stopOnce.Do(func() {
		m.stopped = true
		close(m.stopChan)
	})
}

func (m *blockDatabaseMigrator) runMigration() error {
	log.Info("blockdb migration started")
	iter := m.chaindb.NewIterator(chaindbHeaderPrefix, nil)
	defer iter.Release()
	startTime := time.Now()
	m.lastLogTime = startTime

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

		// skip if not a header key
		if len(key) != len(chaindbHeaderPrefix)+8+32 {
			continue
		}

		blockNum, hash := blockNumberAndHashFromKey(key)

		// don't store non-canonical blocks in blockdb
		canonicalHash := rawdb.ReadCanonicalHash(m.chaindb, blockNum)
		if canonicalHash != hash {
			continue
		}

		body := rawdb.ReadBody(m.chaindb, hash, blockNum)
		bodyBytes, err := rlp.EncodeToBytes(body)
		if err != nil {
			return fmt.Errorf("failed to encode block body: %w", err)
		}
		encodedBlock, err := encodeBlockData(iter.Value(), bodyBytes, hash)
		if err != nil {
			return fmt.Errorf("failed to encode block data: %w", err)
		}
		if err := m.blockdb.WriteBlock(blockNum, encodedBlock); err != nil {
			return fmt.Errorf("failed to write block to blockdb: %w", err)
		}

		batch := m.chaindb.NewBatch()
		// delete the header directly instead of using rawdb.DeleteHeader otherwise
		// the header number mapping will also be deleted.
		headerKey := append(append(chaindbHeaderPrefix, encodeBlockNumber(blockNum)...), hash.Bytes()...)
		if err := batch.Delete(headerKey); err != nil {
			return fmt.Errorf("failed to delete header from chaindb: %w", err)
		}
		rawdb.DeleteBody(batch, hash, blockNum)
		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write batch to chaindb: %w", err)
		}

		blocksProcessed := atomic.AddInt64(&m.blocksProcessed, 1)

		// Log progress every logProgressInterval
		now := time.Now()
		if now.Sub(m.lastLogTime) >= logProgressInterval {
			logFields := []interface{}{
				"blocks_processed", blocksProcessed,
				"last_processed_height", blockNum,
				"time_elapsed", time.Since(startTime),
			}

			if m.headBlockNumber != nil {
				eta := timer.EstimateETA(startTime, blockNum, *m.headBlockNumber)
				logFields = append(logFields, "estimated_end_block", *m.headBlockNumber, "eta", eta.String())
			}

			log.Info("blockdb migration status", logFields...)
			m.lastLogTime = now
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

func (m *blockDatabaseMigrator) getStatus() migrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *blockDatabaseMigrator) getBlocksProcessed() uint64 {
	return uint64(atomic.LoadInt64(&m.blocksProcessed))
}

func (m *blockDatabaseMigrator) saveMigrationStatus(status migrationStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status == status {
		return nil
	}
	m.status = status
	return m.db.Put(migrationStatusKey, []byte{byte(status)})
}

func (m *blockDatabaseMigrator) saveHeadBlockNumber() error {
	if m.headBlockNumber == nil {
		return nil
	}
	blockNumberBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(blockNumberBytes, *m.headBlockNumber)
	return m.db.Put(headBlockNumberKey, blockNumberBytes)
}
