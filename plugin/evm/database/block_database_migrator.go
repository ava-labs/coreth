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

type migrationStatus int

const (
	migrationNotStarted migrationStatus = iota
	migrationInProgress
	migrationCompleted

	logProgressInterval = 5 * time.Minute // Log every 5 minutes
	compactionInterval  = 250_000         // Compact every 250k blocks processed
	batchDeleteInterval = 25_000          // Write delete batch every 25k blocks
)

var (
	// migrationStatusKey stores the persisted progress state for the migrator.
	migrationStatusKey = []byte("migration_status")

	// endBlockNumberKey stores the target block number to migrate to.
	endBlockNumberKey = []byte("migration_end_block_number")
)

// blockDatabaseMigrator migrates canonical block data and receipts from
// ethdb.Database into the height-indexed block and receipt databases.
type blockDatabaseMigrator struct {
	stateDB    database.Database
	chainDB    ethdb.Database
	blockDB    database.HeightIndex
	receiptsDB database.HeightIndex

	status  migrationStatus
	mu      sync.RWMutex // protects status/running/cancel
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	processed uint64
	endHeight uint64
}

// NewBlockDatabaseMigrator creates a new block database migrator with
// current migration status and target migration end block number.
func NewBlockDatabaseMigrator(
	stateDB database.Database,
	blockDB database.HeightIndex,
	receiptsDB database.HeightIndex,
	chainDB ethdb.Database,
) (*blockDatabaseMigrator, error) {
	m := &blockDatabaseMigrator{
		blockDB:    blockDB,
		receiptsDB: receiptsDB,
		stateDB:    stateDB,
		chainDB:    chainDB,
	}

	// load status
	status, err := getMigrationStatus(stateDB)
	if err != nil {
		return nil, err
	}
	m.status = status

	// load end block height
	endHeight, err := getEndBlockHeight(stateDB)
	if err != nil {
		return nil, err
	}

	if endHeight == 0 {
		if endHeight, err = loadAndSaveBlockEndHeight(stateDB, chainDB); err != nil {
			return nil, err
		}
	}
	m.endHeight = endHeight

	return m, nil
}

func (b *blockDatabaseMigrator) Status() migrationStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

func (b *blockDatabaseMigrator) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		// Wait for migration goroutine to finish cleanup
		b.wg.Wait()
	}
}

func (b *blockDatabaseMigrator) Migrate() error {
	if b.status == migrationCompleted {
		return nil
	}
	ctx, err := b.beginRun()
	if err != nil {
		return err
	}

	if err := b.setStatus(migrationInProgress); err != nil {
		b.endRun()
		return err
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer b.endRun()
		if err := b.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Info("migration failed", "err", err)
		}
	}()
	return nil
}

func (b *blockDatabaseMigrator) beginRun() (context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return nil, errors.New("migration already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.running = true
	return ctx, nil
}

func (b *blockDatabaseMigrator) endRun() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancel = nil
	b.running = false
}

func (b *blockDatabaseMigrator) setStatus(s migrationStatus) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status == s {
		return nil
	}
	if err := b.stateDB.Put(migrationStatusKey, []byte{byte(s)}); err != nil {
		return err
	}
	b.status = s
	return nil
}

func (b *blockDatabaseMigrator) run(ctx context.Context) error {
	var (
		etaTarget         uint64
		etaTracker        = timer.NewEtaTracker(10, 1.2)
		startTime         = time.Now()
		timeOfNextLog     = startTime.Add(logProgressInterval)
		deleteBatch       = b.chainDB.NewBatch()
		lastCompactionNum uint64
		firstBlockInRange uint64
		lastBlockInRange  uint64
		// Iterate over block bodies instead of headers since there are keys
		// under the header prefix that we are not migrating.
		iter = b.chainDB.NewIterator(chainDBBlockBodyPrefix, nil)
	)

	// Defer cleanup logic
	defer func() {
		// Release iterator (safe to call multiple times)
		iter.Release()

		// Write any remaining deletes in batch
		if deleteBatch.ValueSize() > 0 {
			if err := deleteBatch.Write(); err != nil {
				log.Error("failed to write final delete batch", "err", err)
			}
		}

		// Compact final range if we processed any blocks
		if lastBlockInRange > 0 {
			b.compactBlockRange(firstBlockInRange, lastBlockInRange)
		}

		// Log final statistics
		processingTime := time.Since(startTime)
		log.Info("blockdb migration completed",
			"blocks_processed", atomic.LoadUint64(&b.processed),
			"total_processing_time", processingTime.String())
	}()

	log.Info("blockdb migration started")

	// iterator will iterate all block headers in ascending order by block number
	for iter.Next() {
		// Check if migration should be stopped
		select {
		case <-ctx.Done():
			log.Info("migration stopped", "blocks_processed", atomic.LoadUint64(&b.processed))
			return ctx.Err()
		default:
			// Continue with migration
		}

		key := iter.Key()
		if !shouldMigrateKey(b.chainDB, key) {
			continue
		}

		blockNum, hash := blockNumberAndHashFromKey(key)

		if etaTarget == 0 && b.endHeight > 0 && blockNum < b.endHeight {
			etaTarget = b.endHeight - blockNum
			etaTracker.AddSample(0, etaTarget, startTime)
		}

		// Track the range of blocks for compaction
		if firstBlockInRange == 0 {
			firstBlockInRange = blockNum
		}
		lastBlockInRange = blockNum

		// Migrate block data (header + body)
		if err := b.migrateBlock(blockNum, hash, iter.Value()); err != nil {
			return fmt.Errorf("failed to migrate block data: %w", err)
		}

		// Migrate receipts
		if err := b.migrateReceipts(blockNum, hash); err != nil {
			return fmt.Errorf("failed to migrate receipt data: %w", err)
		}

		// Add deletes to batch
		if err := b.deleteBlock(deleteBatch, blockNum, hash); err != nil {
			return fmt.Errorf("failed to add block deletes to batch: %w", err)
		}

		processed := atomic.AddUint64(&b.processed, 1)

		// Write delete batch every batchDeleteInterval blocks
		if processed%batchDeleteInterval == 0 {
			if err := deleteBatch.Write(); err != nil {
				return fmt.Errorf("failed to write delete batch: %w", err)
			}
			deleteBatch.Reset()
		}

		// Compact every compactionInterval blocks
		if processed-lastCompactionNum >= compactionInterval {
			log.Info("compaction interval reached, releasing iterator and compacting",
				"blocks_since_last_compaction", processed-lastCompactionNum,
				"total_processed", processed)

			// Write any remaining deletes in batch before compaction
			if deleteBatch.ValueSize() > 0 {
				if err := deleteBatch.Write(); err != nil {
					return fmt.Errorf("failed to write delete batch before compaction: %w", err)
				}
				deleteBatch.Reset()
			}

			// Release iterator before compaction
			iter.Release()

			// Compact the range we just processed
			b.compactBlockRange(firstBlockInRange, lastBlockInRange)

			// Recreate iterator
			start := encodeBlockNumber(blockNum + 1)
			iter = b.chainDB.NewIterator(chainDBBlockBodyPrefix, start)
			lastCompactionNum = processed
			firstBlockInRange = 0
		}

		// Log progress every logProgressInterval
		if now := time.Now(); now.After(timeOfNextLog) {
			logFields := []interface{}{
				"blocks_processed", processed,
				"last_processed_height", blockNum,
				"time_elapsed", time.Since(startTime),
			}
			if b.endHeight != 0 && etaTarget > 0 {
				etaPtr, progressPercentage := etaTracker.AddSample(processed, etaTarget, now)
				if etaPtr != nil {
					logFields = append(logFields, "eta", etaPtr.String())
					logFields = append(logFields, "pctComplete", progressPercentage)
				}
			}

			log.Info("blockdb migration status", logFields...)
			timeOfNextLog = now.Add(logProgressInterval)
		}
	}

	if iter.Error() != nil {
		return fmt.Errorf("failed to iterate over chainDB: %w", iter.Error())
	}

	if err := b.setStatus(migrationCompleted); err != nil {
		log.Error("failed to save completed migration status", "err", err)
	}

	return nil
}

func (b *blockDatabaseMigrator) compactBlockRange(startBlock, endBlock uint64) {
	startTime := time.Now()

	// Compact block headers
	startHeaderKey := blockHeaderKey(startBlock, common.Hash{})
	endHeaderKey := blockHeaderKey(endBlock+1, common.Hash{})
	if err := b.chainDB.Compact(startHeaderKey, endHeaderKey); err != nil {
		log.Error("failed to compact block headers in range", "start_block", startBlock, "end_block", endBlock, "err", err)
	}

	// Compact block bodies
	startBodyKey := blockBodyKey(startBlock, common.Hash{})
	endBodyKey := blockBodyKey(endBlock+1, common.Hash{})
	if err := b.chainDB.Compact(startBodyKey, endBodyKey); err != nil {
		log.Error("failed to compact block bodies in range", "start_block", startBlock, "end_block", endBlock, "err", err)
	}

	// Compact receipts for this range
	startReceiptsKey := receiptsKey(startBlock, common.Hash{})
	endReceiptsKey := receiptsKey(endBlock+1, common.Hash{})
	if err := b.chainDB.Compact(startReceiptsKey, endReceiptsKey); err != nil {
		log.Error("failed to compact receipts in range", "start_block", startBlock, "end_block", endBlock, "err", err)
	}

	log.Info("compaction of block range completed",
		"start_block", startBlock,
		"end_block", endBlock,
		"duration", time.Since(startTime))
}

func (b *blockDatabaseMigrator) migrateBlock(blockNum uint64, hash common.Hash, bodyBytes []byte) error {
	header := rawdb.ReadHeader(b.chainDB, hash, blockNum)
	headerBytes, err := rlp.EncodeToBytes(header)
	if err != nil {
		return fmt.Errorf("failed to encode block header: %w", err)
	}
	encodedBlock, err := encodeBlockData(headerBytes, bodyBytes, hash)
	if err != nil {
		return fmt.Errorf("failed to encode block data: %w", err)
	}
	if err := b.blockDB.Put(blockNum, encodedBlock); err != nil {
		return fmt.Errorf("failed to write block to blockDB: %w", err)
	}
	return nil
}

func (b *blockDatabaseMigrator) migrateReceipts(blockNum uint64, hash common.Hash) error {
	// Read raw receipt bytes directly from chainDB
	receiptBytes := rawdb.ReadReceiptsRLP(b.chainDB, hash, blockNum)
	if receiptBytes == nil {
		// No receipts for this block, skip
		return nil
	}

	encodedReceipts, err := encodeReceiptData(receiptBytes, hash)
	if err != nil {
		return fmt.Errorf("failed to encode receipts with hash: %w", err)
	}
	if err := b.receiptsDB.Put(blockNum, encodedReceipts); err != nil {
		return fmt.Errorf("failed to write receipts to receiptsDB: %w", err)
	}

	return nil
}

// deleteBlock adds delete operations for a block to the provided batch
func (b *blockDatabaseMigrator) deleteBlock(batch ethdb.Batch, blockNum uint64, hash common.Hash) error {
	headerKey := blockHeaderKey(blockNum, hash)
	if err := batch.Delete(headerKey); err != nil {
		return fmt.Errorf("failed to delete header from chainDB: %w", err)
	}
	rawdb.DeleteBody(batch, hash, blockNum)
	rawdb.DeleteReceipts(batch, hash, blockNum)

	return nil
}

func (b *blockDatabaseMigrator) blocksProcessed() uint64 {
	return atomic.LoadUint64(&b.processed)
}

func getMigrationStatus(db database.Database) (migrationStatus, error) {
	var status migrationStatus
	has, err := db.Has(migrationStatusKey)
	if err != nil {
		return status, err
	}
	if !has {
		return status, nil
	}
	b, err := db.Get(migrationStatusKey)
	if err != nil {
		return status, err
	}
	if len(b) != 1 {
		return status, fmt.Errorf("invalid migration status encoding length=%d", len(b))
	}
	return migrationStatus(b[0]), nil
}

func getEndBlockHeight(db database.Database) (uint64, error) {
	has, err := db.Has(endBlockNumberKey)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, nil
	}
	blockNumberBytes, err := db.Get(endBlockNumberKey)
	if err != nil {
		return 0, err
	}
	if len(blockNumberBytes) != blockNumberSize {
		return 0, fmt.Errorf("invalid block number encoding length=%d", len(blockNumberBytes))
	}
	return binary.BigEndian.Uint64(blockNumberBytes), nil
}

func loadAndSaveBlockEndHeight(stateDB database.Database, chainDB ethdb.Database) (uint64, error) {
	headHash := rawdb.ReadHeadHeaderHash(chainDB)
	if headHash == (common.Hash{}) {
		return 0, nil
	}
	headBlockNumber := rawdb.ReadHeaderNumber(chainDB, headHash)
	if headBlockNumber == nil || *headBlockNumber == 0 {
		return 0, nil
	}
	endHeight := *headBlockNumber
	if err := stateDB.Put(endBlockNumberKey, encodeBlockNumber(endHeight)); err != nil {
		return 0, fmt.Errorf("failed to save head block number %d: %w", endHeight, err)
	}
	log.Info("blockdb migration: saved head block number", "head_block_number", endHeight)
	return endHeight, nil
}

func shouldMigrateKey(db ethdb.Database, key []byte) bool {
	if !isBodyKey(key) {
		return false
	}
	blockNum, hash := blockNumberAndHashFromKey(key)

	// Skip genesis blocks to avoid complicating state-sync min-height handling.
	if blockNum == 0 {
		return false
	}

	canonicalHash := rawdb.ReadCanonicalHash(db, blockNum)
	return canonicalHash == hash
}

// blockHeaderKey = headerPrefix + num (uint64 big endian) + hash
func blockHeaderKey(number uint64, hash common.Hash) []byte {
	return append(append(chainDBHeaderPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// blockBodyKey = bodyPrefix + num (uint64 big endian) + hash
func blockBodyKey(number uint64, hash common.Hash) []byte {
	return append(append(chainDBBlockBodyPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// receiptsKey = receiptsPrefix + num (uint64 big endian) + hash
func receiptsKey(number uint64, hash common.Hash) []byte {
	return append(append(chainDBReceiptsPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// minBlockHeightToMigrate returns the smallest block number that should be migrated.
func minBlockHeightToMigrate(db ethdb.Database) *uint64 {
	iter := db.NewIterator(chainDBBlockBodyPrefix, nil)
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
