// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

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
)

// Tests migration status is correctly set to "not started" after initializing a new migrator.
func TestEthDatabaseMigrator_StatusNotStarted(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationStatusNotStarted)
	require.NoError(t, wrapper.Close())
}

// Tests migration status is correctly set to "in progress" during an active migration by using a slow
// block database to ensure the migration doesn't complete immediately.
func TestEthDatabaseMigrator_StatusInProgress(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(chainDB, blocks, receipts)

	// migrator with a slow block database
	slowDB := &slowBlockDatabase{
		BlockDatabase: wrapper.blockDB,
		shouldSlow: func() bool {
			return true
		},
	}
	wrapper.migrator.blockDB = slowDB
	require.NoError(t, wrapper.migrator.Migrate())

	// Wait for it to be in progress
	require.Eventually(t, func() bool {
		return wrapper.migrator.getStatus() == migrationStatusInProgress
	}, 2*time.Second, 100*time.Millisecond)

	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationStatusInProgress)
	require.NoError(t, wrapper.Close())
}

// Tests migration status is correctly set to "completed" after a successful migration.
func TestEthDatabaseMigrator_StatusCompleted(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 3)
	writeBlocks(chainDB, blocks, receipts)
	wrapper.migrator.blockDB = wrapper.blockDB
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 5*time.Second)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationStatusCompleted)
	require.NoError(t, wrapper.Close())
}

// Tests various migration scenarios including consecutive blocks, non-consecutive blocks,
// partial migrations, and edge cases to ensure migration works correctly.
func TestEthDatabaseMigrator_Migration(t *testing.T) {
	testCases := []struct {
		name             string
		initStatus       *migrationStatus
		toMigrateHeights []uint64
		migratedHeights  []uint64
		expStatus        migrationStatus
	}{
		{
			name:             "migrate_5_blocks",
			toMigrateHeights: []uint64{0, 1, 2, 3, 4},
			expStatus:        migrationStatusCompleted,
		},
		{
			name:             "migrate_5_blocks_20_24",
			toMigrateHeights: []uint64{20, 21, 22, 23, 24},
			expStatus:        migrationStatusCompleted,
		},
		{
			name:             "migrate_non_consecutive_blocks",
			toMigrateHeights: []uint64{20, 21, 22, 29, 30, 40},
			expStatus:        migrationStatusCompleted,
		},
		{
			name:             "half_blocks_migrated",
			initStatus:       statusPtr(migrationStatusInProgress),
			toMigrateHeights: []uint64{6, 7, 8, 9, 10},
			migratedHeights:  []uint64{0, 1, 2, 3, 4, 5},
			expStatus:        migrationStatusCompleted,
		},
		{
			name:            "all_blocks_migrated_in_progress",
			initStatus:      statusPtr(migrationStatusInProgress),
			migratedHeights: []uint64{0, 1, 2, 3, 4, 5},
			expStatus:       migrationStatusCompleted,
		},
		{
			name:       "empty_chainDB_no_blocks_to_migrate",
			initStatus: nil,
			expStatus:  migrationStatusCompleted,
		},
		{
			name:             "half_non_consecutive_blocks_migrated",
			initStatus:       statusPtr(migrationStatusInProgress),
			toMigrateHeights: []uint64{2, 3, 7, 8, 10},
			migratedHeights:  []uint64{0, 1, 4, 5, 9},
			expStatus:        migrationStatusCompleted,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			wrapper, chainDB := newDatabasesFromDir(t, dataDir)
			defer wrapper.Close()

			// find the max block height to create
			maxBlockHeight := uint64(0)
			for _, height := range tc.toMigrateHeights {
				if height > maxBlockHeight {
					maxBlockHeight = height
				}
			}
			for _, height := range tc.migratedHeights {
				if height > maxBlockHeight {
					maxBlockHeight = height
				}
			}

			// create blocks and receipts
			blocks, receipts := createBlocks(t, int(maxBlockHeight)+1)
			writeBlocks(chainDB, blocks, receipts)

			// set initial state
			if tc.initStatus != nil {
				require.NoError(t, wrapper.stateDB.Put(migrationStatusKey, []byte{byte(*tc.initStatus)}))
			}
			for i := range tc.toMigrateHeights {
				writeBlocks(
					wrapper,
					[]*types.Block{blocks[tc.toMigrateHeights[i]]},
					[]types.Receipts{receipts[tc.toMigrateHeights[i]]},
				)
			}
			for i := range tc.migratedHeights {
				writeBlocks(
					wrapper,
					[]*types.Block{blocks[tc.migratedHeights[i]]},
					[]types.Receipts{receipts[tc.migratedHeights[i]]},
				)
			}

			// Migrate database
			require.NoError(t, wrapper.migrator.Migrate())
			waitForMigratorCompletion(t, wrapper.migrator, 10*time.Second)
			assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, tc.expStatus)

			// Verify that all blocks and receipts are accessible from the block wrapper
			allExpectedBlocks := make(map[uint64]*types.Block)
			allExpectedReceipts := make(map[uint64]types.Receipts)
			totalBlocks := len(tc.toMigrateHeights) + len(tc.migratedHeights)
			for _, height := range tc.toMigrateHeights {
				allExpectedBlocks[height] = blocks[height]
				allExpectedReceipts[height] = receipts[height]
			}
			for _, height := range tc.migratedHeights {
				allExpectedBlocks[height] = blocks[height]
				allExpectedReceipts[height] = receipts[height]
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
					require.False(t, rawdb.HasHeader(chainDB, expectedBlock.Hash(), expectedBlock.NumberU64()))
					require.False(t, rawdb.HasBody(chainDB, expectedBlock.Hash(), expectedBlock.NumberU64()))
					require.False(t, rawdb.HasReceipts(chainDB, expectedBlock.Hash(), expectedBlock.NumberU64()))
				}
			}
		})
	}
}

// Tests that a migration in progress can be stopped and resumed, verifying that the migration state is properly persisted
// and the migration can continue from where it left off after restart.
func TestEthDatabaseMigrator_AbruptStop(t *testing.T) {
	// Test that a migration in progress can be stopped and resumed
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)

	// Create blocks and write them to chainDB
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(chainDB, blocks, receipts)

	// Create a slow block database that slows down after 3 blocks
	blockCount := 0
	slowBdb := &slowBlockDatabase{
		BlockDatabase: wrapper.blockDB,
		shouldSlow: func() bool {
			blockCount++
			return blockCount > 3
		},
	}

	// Create and start migrator manually with slow block database
	wrapper.migrator.blockDB = slowBdb
	require.NoError(t, wrapper.migrator.Migrate())

	// trigger db close after 3 blocks are migrated
	require.Eventually(t, func() bool {
		return wrapper.migrator.getBlocksProcessed() >= 3
	}, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, wrapper.Close())

	// Create new wrapper and verify migration status
	wrapper, chainDB = newDatabasesFromDir(t, dataDir)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationStatusInProgress)

	// Check blockdb and chainDB to ensure we have the correct amount of blocks
	chainDBBlocks := 0
	blockdbBlocks := 0
	wrapperBlocks := 0
	for _, block := range blocks {
		if rawdb.HasHeader(chainDB, block.Hash(), block.NumberU64()) {
			chainDBBlocks++
		}
		if has, _ := wrapper.blockDB.HasBlock(block.NumberU64()); has {
			blockdbBlocks++
		}
		if rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()) {
			wrapperBlocks++
		}
	}
	require.Equal(t, 10, chainDBBlocks+blockdbBlocks)
	require.Equal(t, 10, wrapperBlocks)

	// Start migration again to resume
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 15*time.Second)
	assertMigrationStatus(t, wrapper.migrator.stateDB, wrapper.migrator, migrationStatusCompleted)

	// Verify that all blocks are accessible from the new wrapper
	for i, block := range blocks {
		actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
		require.NotNil(t, actualBlock, "Block %d should be accessible after resumption", block.NumberU64())
		assertRLPEqual(t, block, actualBlock)
		actualReceipts := rawdb.ReadReceipts(wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		assertRLPEqual(t, receipts[i], actualReceipts)

		// Verify chainDB no longer has blocks except for genesis
		if i != 0 {
			require.False(t, rawdb.HasHeader(chainDB, block.Hash(), block.NumberU64()))
			require.False(t, rawdb.HasBody(chainDB, block.Hash(), block.NumberU64()))
			require.False(t, rawdb.HasReceipts(chainDB, block.Hash(), block.NumberU64()))
		}
	}

	require.NoError(t, wrapper.Close())
}

// Test that the genesis block is not migrated, rest of blocks should be migrated
func TestEthDatabaseMigrator_Genesis(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	chainDB := rawdb.NewDatabase(WrapDatabase(base))
	blocks, receipts := createBlocks(t, 10)

	// Write only genesis and block 5-9
	writeBlocks(chainDB, blocks[0:1], receipts[0:1])
	writeBlocks(chainDB, blocks[5:10], receipts[5:10])

	// create wrapper
	blockDBPath := filepath.Join(dataDir, "blockdb")
	wrapper, err := NewWrappedEthDatabase(base, blockDBPath, chainDB, false, blockdb.DefaultConfig(), false, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)

	// validate our min height is 5 since we don't store genesis block
	require.True(t, wrapper.IsInitialized())
	require.Equal(t, uint64(5), *wrapper.minHeight)

	// migrate
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 10*time.Second)

	// verify genesis block is not migrated
	genesisHash := rawdb.ReadCanonicalHash(chainDB, 0)
	require.True(t, rawdb.HasHeader(chainDB, genesisHash, 0))
	has, _ := wrapper.blockDB.HasBlock(0)
	require.False(t, has)

	// verify blocks 1-4 are missing
	for i := 1; i < 5; i++ {
		hash := rawdb.ReadCanonicalHash(wrapper, uint64(i))
		require.Equal(t, common.Hash{}, hash)
	}

	// verify blocks 5-9 are migrated
	for _, block := range blocks[5:10] {
		block := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
		require.NotNil(t, block)
		assertRLPEqual(t, block, block)
		has, err := wrapper.blockDB.HasBlock(block.NumberU64())
		require.NoError(t, err)
		require.True(t, has)
	}
}

func waitForMigratorCompletion(t *testing.T, migrator *ethDatabaseMigrator, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		if migrator.getStatus() == migrationStatusCompleted {
			return
		}
	}

	t.Fatalf("migration did not complete within %v", timeout)
}

func assertMigrationStatus(t *testing.T, base database.Database, migrator *ethDatabaseMigrator, expectedStatus migrationStatus) {
	t.Helper()

	require.Equal(t, expectedStatus, migrator.getStatus(), "migrator status should match expected")
	status, err := getMigrationStatus(base)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, status, "base database status should match expected")
}

func statusPtr(status migrationStatus) *migrationStatus {
	return &status
}
