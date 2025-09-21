// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type slowBlockDatabase struct {
	database.BlockDatabase
	shouldSlow func() bool
}

func (s *slowBlockDatabase) WriteBlock(blockNumber uint64, encodedBlock []byte) error {
	// Sleep to make migration hang for a bit
	if s.shouldSlow == nil || s.shouldSlow() {
		time.Sleep(1 * time.Second)
	}
	return s.BlockDatabase.WriteBlock(blockNumber, encodedBlock)
}

func TestBlockDatabaseMigrator_Status(t *testing.T) {
	t.Run("Migration status is not started after initialization", func(t *testing.T) {
		base := memdb.New()
		bdb := blockdb.NewMemoryDatabase()
		chaindb := rawdb.NewDatabase(WrapDatabase(base))
		migrator, err := NewBlockDatabaseMigrator(base, bdb, chaindb)
		require.NoError(t, err)
		require.Equal(t, migrationStatusNotStarted, migrator.getStatus())
	})

	t.Run("Migration status is in progress during migration", func(t *testing.T) {
		base := memdb.New()
		bdb := blockdb.NewMemoryDatabase()
		chaindb := rawdb.NewDatabase(WrapDatabase(base))
		blocks, _ := createBlocks(t, 10)
		writeKVDBBlocks(chaindb, blocks)
		slowBdb := &slowBlockDatabase{BlockDatabase: bdb}
		migrator, err := NewBlockDatabaseMigrator(base, slowBdb, chaindb)
		require.NoError(t, err)
		require.Equal(t, migrationStatusNotStarted, migrator.getStatus())
		require.NoError(t, migrator.Migrate())
		require.Eventually(t, func() bool {
			return migrator.getStatus() == migrationStatusInProgress
		}, 2*time.Second, 100*time.Millisecond)
		migrator.Stop()
	})

	t.Run("Migration status is completed after migration", func(t *testing.T) {
		base := memdb.New()
		bdb := blockdb.NewMemoryDatabase()
		chaindb := rawdb.NewDatabase(WrapDatabase(base))
		blocks, _ := createBlocks(t, 10)
		writeKVDBBlocks(chaindb, blocks)
		migrator, err := NewBlockDatabaseMigrator(base, bdb, chaindb)
		require.NoError(t, err)
		require.NoError(t, migrator.Migrate())
		waitForMigratorCompletion(t, migrator, 5*time.Second)
		require.Equal(t, migrationStatusCompleted, migrator.getStatus())
	})
}

func TestWrappedBlockDatabase_Migration(t *testing.T) {
	testCases := []struct {
		name       string
		initStatus *migrationStatus
		getBlocks  func(t *testing.T) (kvdbBlocks []*types.Block, blockdbBlocks []*types.Block)
		expStatus  migrationStatus
	}{
		{
			name: "Migrate 10 blocks from 1-10",
			getBlocks: func(t *testing.T) ([]*types.Block, []*types.Block) {
				blocks, _ := createBlocks(t, 10)
				return blocks, []*types.Block{}
			},
			expStatus: migrationStatusCompleted,
		},
		{
			name: "Migrate 10 blocks from 20-30",
			getBlocks: func(t *testing.T) ([]*types.Block, []*types.Block) {
				blocks, _ := createBlocks(t, 30)
				return blocks[20:30], []*types.Block{}
			},
			expStatus: migrationStatusCompleted,
		},
		{
			name: "Migrate non-consecutive blocks (20-30 with 25,28 missing)",
			getBlocks: func(t *testing.T) ([]*types.Block, []*types.Block) {
				blocks, _ := createBlocks(t, 30)
				kvdbBlocks := make([]*types.Block, 0)
				for i := 20; i < 30; i++ {
					if i != 25 && i != 28 {
						kvdbBlocks = append(kvdbBlocks, blocks[i])
					}
				}
				return kvdbBlocks, []*types.Block{}
			},
			expStatus: migrationStatusCompleted,
		},
		{
			name:       "Half of blocks already migrated (1-5 migrated, 6-10 need migration)",
			initStatus: statusPtr(migrationStatusInProgress),
			getBlocks: func(t *testing.T) ([]*types.Block, []*types.Block) {
				blocks, _ := createBlocks(t, 10)
				return blocks[5:10], blocks[0:5]
			},
			expStatus: migrationStatusCompleted,
		},
		{
			name:       "All blocks already migrated but status in progress",
			initStatus: statusPtr(migrationStatusInProgress),
			getBlocks: func(t *testing.T) ([]*types.Block, []*types.Block) {
				blocks, _ := createBlocks(t, 10)
				return []*types.Block{}, blocks
			},
			expStatus: migrationStatusCompleted,
		},
		{
			name:       "Empty chaindb (no blocks to migrate)",
			initStatus: nil,
			getBlocks: func(_ *testing.T) ([]*types.Block, []*types.Block) {
				return []*types.Block{}, []*types.Block{}
			},
			expStatus: migrationStatusCompleted,
		},
		{
			name:       "Migration from in progress status",
			initStatus: statusPtr(migrationStatusInProgress),
			getBlocks: func(t *testing.T) ([]*types.Block, []*types.Block) {
				blocks, _ := createBlocks(t, 10)
				var migratedBlocks []*types.Block
				var leftBlocks []*types.Block
				for i, block := range blocks {
					if i == 2 || i == 5 {
						migratedBlocks = append(migratedBlocks, block)
					} else {
						leftBlocks = append(leftBlocks, block)
					}
				}

				return leftBlocks, migratedBlocks
			},
			expStatus: migrationStatusCompleted,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			base, chaindb, bdb := newDatabasesFromDir(t, dataDir)

			// Get blocks for KVDB and BlockDB
			kvdbBlocks, blockdbBlocks := tc.getBlocks(t)

			// create a blockdb wrapper without migration so we can add blocks
			wrappedDB, err := NewWrappedBlockDatabase(base, bdb, chaindb, false)
			require.NoError(t, err)

			// set the initial migration status
			if tc.initStatus != nil {
				require.NoError(t, base.Put(migrationStatusKey, []byte{byte(*tc.initStatus)}))
			}

			// Write blocks to BlockDB and KVDB to prepare for migration
			for _, block := range blockdbBlocks {
				rawdb.WriteBlock(wrappedDB, block)
			}
			writeKVDBBlocks(chaindb, kvdbBlocks)
			require.NoError(t, wrappedDB.Close())

			// Create a new wrapped block database to start migration
			base, chaindb, bdb = newDatabasesFromDir(t, dataDir)
			newWrapper, err := NewWrappedBlockDatabase(base, bdb, chaindb, true)
			require.NoError(t, err)
			waitForMigratorCompletion(t, newWrapper.migrator, 10*time.Second)

			// Verify final migration status by checking the migrator database
			status, err := base.Get(migrationStatusKey)
			require.NoError(t, err)
			require.Equal(t, tc.expStatus, migrationStatus(status[0]))

			// Verify that all blocks are accessible from the block wrapper
			allExpectedBlocks := make(map[uint64]*types.Block)
			totalBlocks := len(kvdbBlocks) + len(blockdbBlocks)
			for _, block := range kvdbBlocks {
				allExpectedBlocks[block.NumberU64()] = block
			}
			for _, block := range blockdbBlocks {
				allExpectedBlocks[block.NumberU64()] = block
			}
			require.Equal(t, totalBlocks, len(allExpectedBlocks))
			for blockNum, expectedBlock := range allExpectedBlocks {
				actualBlock := rawdb.ReadBlock(newWrapper, expectedBlock.Hash(), blockNum)
				require.NotNil(t, actualBlock, "Block %d should be accessible", blockNum)
				require.Equal(t, expectedBlock.NumberU64(), actualBlock.NumberU64())
				assertRLPEqual(t, expectedBlock, actualBlock)

				// verify chaindb no longer has any blocks
				require.False(t, rawdb.HasHeader(chaindb, expectedBlock.Hash(), expectedBlock.NumberU64()))
				require.False(t, rawdb.HasBody(chaindb, expectedBlock.Hash(), expectedBlock.NumberU64()))
			}
			require.NoError(t, newWrapper.Close())
		})
	}
}

func TestBlockDatabaseMigrator_AbruptStop(t *testing.T) {
	dataDir := t.TempDir()
	base, chaindb, bdb := newDatabasesFromDir(t, dataDir)

	// Create blocks and write them to chaindb
	blocks, _ := createBlocks(t, 10)
	writeKVDBBlocks(chaindb, blocks)

	// Create a slow block database that slows down after 3 blocks
	blockCount := 0
	slowBdb := &slowBlockDatabase{
		BlockDatabase: bdb,
		shouldSlow: func() bool {
			blockCount++
			return blockCount > 3
		},
	}
	wrapper, err := NewWrappedBlockDatabase(base, slowBdb, chaindb, true)
	require.NoError(t, err)
	require.NotNil(t, wrapper.migrator)

	// wait for 3 blocks are processed before closing the wrapper
	require.Eventually(t, func() bool {
		return wrapper.migrator.getBlocksProcessed() == 3
	}, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, wrapper.Close())

	// Verify migration status
	base, chaindb, bdb = newDatabasesFromDir(t, dataDir)
	status, err := base.Get(migrationStatusKey)
	require.NoError(t, err)
	require.Equal(t, migrationStatusInProgress, migrationStatus(status[0]))

	// Check blockdb and chaindb to ensure we have the correct amount of blocks
	// Count blocks in blockdb
	bdbWrapper, err := NewWrappedBlockDatabase(base, bdb, chaindb, false)
	require.NoError(t, err)

	chaindbBlocks := 0
	blockdbBlocks := 0
	for _, block := range blocks {
		if rawdb.HasHeader(chaindb, block.Hash(), block.NumberU64()) {
			chaindbBlocks++
		}
		if rawdb.HasHeader(bdbWrapper, block.Hash(), block.NumberU64()) {
			blockdbBlocks++
		}
	}
	require.Equal(t, 10, chaindbBlocks+blockdbBlocks)

	// Create new wrapper to resume migration
	newWrapper, err := NewWrappedBlockDatabase(base, bdb, chaindb, true)
	require.NoError(t, err)
	waitForMigratorCompletion(t, newWrapper.migrator, 15*time.Second)

	// Verify final migration status
	status, err = base.Get(migrationStatusKey)
	require.NoError(t, err)
	require.Equal(t, migrationStatusCompleted, migrationStatus(status[0]))

	// Verify that all blocks are accessible from the new wrapper
	for _, block := range blocks {
		actualBlock := rawdb.ReadBlock(newWrapper, block.Hash(), block.NumberU64())
		require.NotNil(t, actualBlock, "Block %d should be accessible after resumption", block.NumberU64())
		require.Equal(t, block.NumberU64(), actualBlock.NumberU64())
		assertRLPEqual(t, block, actualBlock)

		// Verify chaindb no longer has blocks (they should have been migrated)
		require.False(t, rawdb.HasHeader(chaindb, block.Hash(), block.NumberU64()))
		require.False(t, rawdb.HasBody(chaindb, block.Hash(), block.NumberU64()))
	}

	require.NoError(t, newWrapper.Close())
}

func waitForMigratorCompletion(t *testing.T, migrator *blockDatabaseMigrator, timeout time.Duration) {
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

func statusPtr(status migrationStatus) *migrationStatus {
	return &status
}

func newDatabasesFromDir(t *testing.T, dataDir string) (database.Database, ethdb.Database, database.BlockDatabase) {
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	bdb, err := blockdb.New(blockdb.DefaultConfig().WithDir(dataDir), logging.NoLog{})
	require.NoError(t, err)
	chaindb := rawdb.NewDatabase(WrapDatabase(base))
	return base, chaindb, bdb
}

func writeKVDBBlocks(chaindb ethdb.Database, blocks []*types.Block) {
	for _, block := range blocks {
		rawdb.WriteBlock(chaindb, block)
		// Write canonical hash to ensure blocks can be migrated
		rawdb.WriteCanonicalHash(chaindb, block.Hash(), block.NumberU64())
	}
}
