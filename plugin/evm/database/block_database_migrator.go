package database

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
)

var migrationStatusKey = []byte("migration_status")

type migrationStatus int

const (
	migrationStatusNotStarted migrationStatus = iota
	migrationStatusInProgress
	migrationStatusCompleted
)

const (
	progressInterval    = 5 * time.Second
	logProgressInterval = 1000 // Log every 1000 blocks processed
)

type blockDatabaseMigrator struct {
	// db is the database for the migrator to store any state
	db                 database.Database
	blockdb            database.BlockDatabase
	chaindb            ethdb.Database
	status             migrationStatus
	lastMigratedHeight *uint64
	startTime          time.Time
	blocksProcessed    uint64
	stopChan           chan struct{}
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

func NewBlockDatabaseMigrator(db database.Database, blockdb database.BlockDatabase, chaindb ethdb.Database) (*blockDatabaseMigrator, error) {
	mStatus, err := getMigrationStatus(db)
	if err != nil {
		return nil, err
	}

	return &blockDatabaseMigrator{
		blockdb:  blockdb,
		db:       db,
		chaindb:  chaindb,
		status:   mStatus,
		stopChan: make(chan struct{}),
	}, nil
}

// Migrate starts the migration process
func (m *blockDatabaseMigrator) Migrate() error {
	if m.status == migrationStatusCompleted {
		return nil
	}

	// Start migration if not started or in progress
	if m.status == migrationStatusNotStarted || m.status == migrationStatusInProgress {
		// Initialize migration state
		m.startTime = time.Now()
		m.blocksProcessed = 0
		m.status = migrationStatusInProgress

		// Save status to indicate migration is in progress
		if err := m.saveMigrationStatus(migrationStatusInProgress); err != nil {
			return err
		}

		// Start migration in background
		go func() {
			if err := m.runMigration(); err != nil {
				log.Error("migration failed", "err", err)
			}
		}()
	}

	return nil
}

// Stop stops the migration process
func (m *blockDatabaseMigrator) Stop() {
	close(m.stopChan)
}

func (m *blockDatabaseMigrator) runMigration() error {
	log.Info("blockdb starting migration")
	iter := m.chaindb.NewIterator(chaindbHeaderPrefix, nil)
	defer iter.Release()

	// iterator will iterate all block headers in ascending order by block number
	for iter.Next() {
		// Check if migration should be stopped
		select {
		case <-m.stopChan:
			log.Info("migration stopped by user", "blocks_processed", m.blocksProcessed)
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
		encodedBlock, err := encodeBlockData(iter.Value(), bodyBytes)
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

		m.lastMigratedHeight = &blockNum
		m.blocksProcessed++

		// Log progress every logProgressInterval blocks
		if m.blocksProcessed%logProgressInterval == 0 {
			log.Info("blockdb migration progress", "blocks_processed", m.blocksProcessed, "last_processed_height", blockNum, "time_elapsed", time.Since(m.startTime))
		}
	}

	// Migration completed successfully
	if err := m.saveMigrationStatus(migrationStatusCompleted); err != nil {
		log.Error("failed to save completed migration status", "err", err)
	}

	// Log final statistics
	processingTime := time.Since(m.startTime)
	log.Info("migration completed",
		"blocks_processed", m.blocksProcessed,
		"total_processing_time", processingTime.String())

	return nil
}

func (m *blockDatabaseMigrator) saveMigrationStatus(status migrationStatus) error {
	m.status = status
	return m.db.Put(migrationStatusKey, []byte{byte(status)})
}
