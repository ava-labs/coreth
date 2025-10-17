// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	evmdb "github.com/ava-labs/coreth/plugin/evm/database"
)

// Tests migration status is correctly set to "not started" after initializing a new migrator.
func TestMigrator_StatusNotStarted(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	t.Cleanup(func() {
		require.NoError(t, wrapper.Close())
	})
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationNotStarted)
}

// Tests migration status transitions to "in progress" while a migration is actively running.
func TestMigrator_StatusInProgress(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	t.Cleanup(func() {
		require.NoError(t, wrapper.Close())
	})
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(chainDB, blocks, receipts)

	// migrate with a slow block database
	slowDB := &slowDatabase{
		HeightIndex: wrapper.bodyDB,
		shouldSlow: func() bool {
			return true
		},
	}
	wrapper.migrator.bodyDB = slowDB
	require.NoError(t, wrapper.migrator.Migrate())

	// Wait for it to be in progress
	require.Eventually(t, func() bool {
		return wrapper.migrator.Status() == migrationInProgress
	}, 2*time.Second, 100*time.Millisecond)

	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationInProgress)
}

// Tests migration status is correctly set to "completed" after a successful migration.
func TestMigrator_StatusCompleted(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	t.Cleanup(func() {
		require.NoError(t, wrapper.Close())
	})

	blocks, receipts := createBlocks(t, 3)
	writeBlocks(chainDB, blocks, receipts)
	wrapper.migrator.bodyDB = wrapper.bodyDB
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 5*time.Second)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationCompleted)
}

func TestMigrator_Migration(t *testing.T) {
	testCases := []struct {
		name             string
		initStatus       migrationStatus
		toMigrateHeights []uint64
		migratedHeights  []uint64
		expStatus        migrationStatus
	}{
		{
			name:             "migrate_5_blocks",
			toMigrateHeights: []uint64{0, 1, 2, 3, 4},
			expStatus:        migrationCompleted,
		},
		{
			name:             "migrate_5_blocks_20_24",
			toMigrateHeights: []uint64{20, 21, 22, 23, 24},
			expStatus:        migrationCompleted,
		},
		{
			name:             "migrate_non_consecutive_blocks",
			toMigrateHeights: []uint64{20, 21, 22, 29, 30, 40},
			expStatus:        migrationCompleted,
		},
		{
			name:             "half_blocks_migrated",
			initStatus:       migrationInProgress,
			toMigrateHeights: []uint64{6, 7, 8, 9, 10},
			migratedHeights:  []uint64{0, 1, 2, 3, 4, 5},
			expStatus:        migrationCompleted,
		},
		{
			name:            "all_blocks_migrated_in_progress",
			initStatus:      migrationInProgress,
			migratedHeights: []uint64{0, 1, 2, 3, 4, 5},
			expStatus:       migrationCompleted,
		},
		{
			name:      "empty_chainDB_no_blocks_to_migrate",
			expStatus: migrationCompleted,
		},
		{
			name:             "half_non_consecutive_blocks_migrated",
			initStatus:       migrationInProgress,
			toMigrateHeights: []uint64{2, 3, 7, 8, 10},
			migratedHeights:  []uint64{0, 1, 4, 5, 9},
			expStatus:        migrationCompleted,
		},
		{
			name:            "migration_already_completed",
			initStatus:      migrationCompleted,
			migratedHeights: []uint64{0, 1, 2},
			expStatus:       migrationCompleted,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			wrapper, chainDB := newDatabasesFromDir(t, dataDir)
			t.Cleanup(func() {
				require.NoError(t, wrapper.Close())
			})

			// find the max block height to create
			maxBlockHeight := uint64(0)
			heightSets := [][]uint64{tc.toMigrateHeights, tc.migratedHeights}
			for _, heights := range heightSets {
				for _, height := range heights {
					if height > maxBlockHeight {
						maxBlockHeight = height
					}
				}
			}

			// create blocks and receipts
			blocks, receipts := createBlocks(t, int(maxBlockHeight)+1)

			// set initial state
			if tc.initStatus != migrationNotStarted {
				require.NoError(t, wrapper.migrator.setStatus(tc.initStatus))
			}
			for _, height := range tc.toMigrateHeights {
				writeBlocks(chainDB, []*types.Block{blocks[height]}, []types.Receipts{receipts[height]})
			}
			for _, height := range tc.migratedHeights {
				writeBlocks(wrapper, []*types.Block{blocks[height]}, []types.Receipts{receipts[height]})
			}

			// Migrate database
			require.NoError(t, wrapper.migrator.Migrate())
			waitForMigratorCompletion(t, wrapper.migrator, 10*time.Second)
			assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, tc.expStatus)

			// Verify that all blocks and receipts are accessible from the block wrapper
			totalBlocks := len(tc.toMigrateHeights) + len(tc.migratedHeights)
			allExpectedBlocks := make(map[uint64]*types.Block, totalBlocks)
			allExpectedReceipts := make(map[uint64]types.Receipts, totalBlocks)
			for _, heights := range heightSets {
				for _, height := range heights {
					allExpectedBlocks[height] = blocks[height]
					allExpectedReceipts[height] = receipts[height]
				}
			}
			require.Len(t, allExpectedBlocks, totalBlocks)
			for blockNum, expectedBlock := range allExpectedBlocks {
				actualBlock := rawdb.ReadBlock(wrapper, expectedBlock.Hash(), blockNum)
				require.NotNil(t, actualBlock, "Block %d should be accessible", blockNum)
				assertRLPEqual(t, expectedBlock, actualBlock)
				expectedReceipts := allExpectedReceipts[blockNum]
				actualReceipts := rawdb.ReadReceipts(wrapper, expectedBlock.Hash(), blockNum, expectedBlock.Time(), params.TestChainConfig)
				assertRLPEqual(t, expectedReceipts, actualReceipts)

				// verify chainDB no longer has any blocks or receipts (except for genesis)
				if expectedBlock.NumberU64() != 0 {
					require.False(t, rawdb.HasHeader(chainDB, expectedBlock.Hash(), expectedBlock.NumberU64()), "Block %d should not be in chainDB", expectedBlock.NumberU64())
					require.False(t, rawdb.HasBody(chainDB, expectedBlock.Hash(), expectedBlock.NumberU64()))
					require.False(t, rawdb.HasReceipts(chainDB, expectedBlock.Hash(), expectedBlock.NumberU64()))
				}
			}
		})
	}
}

// Tests that a migration in progress can be stopped and resumed, verifying
// that the migration state is properly persisted and that the migration
// can continue from where it left off after restart.
func TestMigrator_AbruptStop(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	t.Cleanup(func() {
		if wrapper != nil {
			require.NoError(t, wrapper.Close())
		}
	})

	// Create blocks and write them to chainDB
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(chainDB, blocks, receipts)

	// Create a slow block database that slows down after 3 blocks
	blockCount := 0
	slowBdb := &slowDatabase{
		HeightIndex: wrapper.bodyDB,
		shouldSlow: func() bool {
			blockCount++
			return blockCount > 3
		},
	}

	// Start migration with slow block database and wait for 3 blocks to be migrated
	wrapper.migrator.bodyDB = slowBdb
	require.NoError(t, wrapper.migrator.Migrate())
	require.Eventually(t, func() bool {
		return wrapper.migrator.blocksProcessed() >= 3
	}, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, wrapper.Close())

	// Create new wrapper and verify migration status
	wrapper, chainDB = newDatabasesFromDir(t, dataDir)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationInProgress)

	// Check bodyDB and chainDB to ensure we have the correct amount of blocks
	chainDBBlocks := 0
	blockdbBlocks := 0
	wrapperBlocks := 0
	for _, block := range blocks {
		if rawdb.HasHeader(chainDB, block.Hash(), block.NumberU64()) {
			chainDBBlocks++
		}
		has, err := wrapper.bodyDB.Has(block.NumberU64())
		require.NoError(t, err)
		if has {
			blockdbBlocks++
		}
		if rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()) {
			wrapperBlocks++
		}
	}
	require.Equal(t, 10, chainDBBlocks+blockdbBlocks)
	require.Equal(t, 10, wrapperBlocks)

	// Start migration again and finish
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 15*time.Second)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationCompleted)

	// Verify that all blocks are accessible from the new wrapper
	for i, block := range blocks {
		actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
		require.NotNil(t, actualBlock, "Block %d should be accessible after resumption", block.NumberU64())
		assertRLPEqual(t, block, actualBlock)
		actualReceipts := rawdb.ReadReceipts(wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		assertRLPEqual(t, receipts[i], actualReceipts)

		// chainDB no longer has blocks except for genesis
		if block.NumberU64() != 0 {
			require.False(t, rawdb.HasHeader(chainDB, block.Hash(), block.NumberU64()))
			require.False(t, rawdb.HasBody(chainDB, block.Hash(), block.NumberU64()))
			require.False(t, rawdb.HasReceipts(chainDB, block.Hash(), block.NumberU64()))
		}
	}
}

// Test that the genesis block is not migrated; the rest of the blocks should be migrated.
func TestMigrator_Genesis(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	chainDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))
	blocks, receipts := createBlocks(t, 10)

	// Write only genesis and block 5-9
	writeBlocks(chainDB, blocks[0:1], receipts[0:1])
	writeBlocks(chainDB, blocks[5:10], receipts[5:10])

	blockDBPath := filepath.Join(dataDir, "blockdb")
	wrapper := New(base, chainDB, blockdb.DefaultConfig(), blockDBPath, logging.NoLog{}, prometheus.NewRegistry())
	initialized, err := wrapper.InitWithStateSync(false)
	require.NoError(t, err)
	require.True(t, initialized)
	t.Cleanup(func() {
		require.NoError(t, wrapper.Close())
	})

	// validate our min height is 5 since we don't store genesis block and first
	// block to migrate is block 5.
	require.True(t, wrapper.initialized)
	require.Equal(t, uint64(5), wrapper.minHeight)

	// migrate and wait for completion
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 10*time.Second)

	// verify genesis block is not migrated
	genesisHash := rawdb.ReadCanonicalHash(chainDB, 0)
	require.True(t, rawdb.HasHeader(chainDB, genesisHash, 0))
	has, _ := wrapper.bodyDB.Has(0)
	require.False(t, has)

	// verify blocks 1-4 are missing
	for i := 1; i < 5; i++ {
		hash := rawdb.ReadCanonicalHash(wrapper, uint64(i))
		require.Equal(t, common.Hash{}, hash)
	}

	// verify blocks 5-9 are migrated
	for _, expectedBlock := range blocks[5:10] {
		actualBlock := rawdb.ReadBlock(wrapper, expectedBlock.Hash(), expectedBlock.NumberU64())
		require.NotNil(t, actualBlock)
		assertRLPEqual(t, expectedBlock, actualBlock)
		has, err := wrapper.bodyDB.Has(expectedBlock.NumberU64())
		require.NoError(t, err)
		require.True(t, has)
	}
}

func waitForMigratorCompletion(t *testing.T, migrator *migrator, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		if migrator.Status() == migrationCompleted {
			return
		}
		<-ticker.C
	}

	require.Failf(t, "migration did not complete within timeout", "timeout: %v", timeout)
}

func assertMigrationStatus(t *testing.T, db database.Database, migrator *migrator, expectedStatus migrationStatus) {
	t.Helper()

	require.Equal(t, expectedStatus, migrator.Status(), "migrator status should match expected")
	diskStatus, err := getMigrationStatus(db)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, diskStatus, "disk database status should match expected")
}
