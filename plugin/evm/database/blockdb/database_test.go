// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	evmdb "github.com/ava-labs/coreth/plugin/evm/database"
)

var (
	key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	addr1   = crypto.PubkeyToAddress(key1.PublicKey)
	addr2   = crypto.PubkeyToAddress(key2.PublicKey)
)

// slowDatabase wraps a HeightIndex to add artificial delays
type slowDatabase struct {
	database.HeightIndex
	shouldSlow func() bool
}

func (s *slowDatabase) Put(blockNumber uint64, encodedBlock []byte) error {
	// Sleep to make migration hang for a bit
	if s.shouldSlow == nil || s.shouldSlow() {
		time.Sleep(100 * time.Millisecond)
	}
	return s.HeightIndex.Put(blockNumber, encodedBlock)
}

func newDatabasesFromDir(t *testing.T, dataDir string) (*Database, ethdb.Database) {
	t.Helper()

	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	chainDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))
	wrapper := New(base, chainDB, blockdb.DefaultConfig(), dataDir, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, wrapper.InitWithMinHeight(1))

	return wrapper, chainDB
}

func createBlocks(t *testing.T, numBlocks int) ([]*types.Block, []types.Receipts) {
	t.Helper()

	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Number: 0,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	signer := types.LatestSigner(params.TestChainConfig)
	db, blocks, receipts, err := core.GenerateChainWithGenesis(gspec, engine, numBlocks-1, 10, func(_ int, gen *core.BlockGen) {
		tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID:   params.TestChainConfig.ChainID,
			Nonce:     gen.TxNonce(addr1),
			To:        &addr2,
			Gas:       500000,
			GasTipCap: big.NewInt(1),
			GasFeeCap: big.NewInt(1),
		}), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(t, err)

	// add genesis block
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	genesisBlock := rawdb.ReadBlock(db, genesisHash, 0)
	genesisReceipts := rawdb.ReadReceipts(db, genesisHash, 0, 0, params.TestChainConfig)
	blocks = slices.Concat(blocks, []*types.Block{genesisBlock})
	receipts = slices.Concat(receipts, []types.Receipts{genesisReceipts})

	return blocks, receipts
}

func writeBlocks(db ethdb.Database, blocks []*types.Block, receipts []types.Receipts) {
	for i, block := range blocks {
		rawdb.WriteBlock(db, block)
		if len(receipts) > 0 {
			rawdb.WriteReceipts(db, block.Hash(), block.NumberU64(), receipts[i])
		}
		rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	}
}

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

func TestDatabaseReadBlock(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(wrapper, blocks, receipts)

	for _, block := range blocks {
		actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
		assertRLPEqual(t, block, actualBlock)
	}
}

func TestDatabaseReadReceipts(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(wrapper, blocks, receipts)

	for i, block := range blocks {
		actualReceipts := rawdb.ReadReceipts(wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		assertRLPEqual(t, receipts[i], actualReceipts)
	}
}

func TestDatabaseReadLogs(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(wrapper, blocks, receipts)

	for i, block := range blocks {
		actualLogs := rawdb.ReadLogs(wrapper, block.Hash(), block.NumberU64())
		blockReceipts := receipts[i]
		expectedLogs := make([][]*types.Log, len(blockReceipts))
		for j, receipt := range blockReceipts {
			expectedLogs[j] = receipt.Logs
		}
		assertRLPEqual(t, expectedLogs, actualLogs)
	}
}

// TestDatabaseDelete verifies that block header, body and receipts cannot be deleted,
// but hash to height mapping should be deleted.
func TestDatabaseDelete(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 4)
	targetBlocks := blocks[1:]
	targetReceipts := receipts[1:]
	writeBlocks(wrapper, targetBlocks, targetReceipts)

	for i, block := range targetBlocks {
		// we cannot delete header or body
		rawdb.DeleteBlock(wrapper, block.Hash(), block.NumberU64())
		require.True(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
		require.True(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))
		header := rawdb.ReadHeader(wrapper, block.Hash(), block.NumberU64())
		assertRLPEqual(t, block.Header(), header)
		body := rawdb.ReadBody(wrapper, block.Hash(), block.NumberU64())
		assertRLPEqual(t, block.Body(), body)

		// Verify hash to height mapping is deleted
		blockNumber := rawdb.ReadHeaderNumber(wrapper, block.Hash())
		require.Nil(t, blockNumber)

		// Receipts and logs should not be deleted
		expectedReceipts := targetReceipts[i]
		recs := rawdb.ReadReceipts(wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		assertRLPEqual(t, expectedReceipts, recs)
		logs := rawdb.ReadLogs(wrapper, block.Hash(), block.NumberU64())
		expectedLogs := make([][]*types.Log, len(expectedReceipts))
		for j, receipt := range expectedReceipts {
			expectedLogs[j] = receipt.Logs
		}
		assertRLPEqual(t, expectedLogs, logs)
	}
}

func TestDatabaseWrite(t *testing.T) {
	// Test that header and body are stored separately and block can be read
	// after both are written.
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	blocks, _ := createBlocks(t, 2)
	block := blocks[1]

	rawdb.WriteHeader(wrapper, block.Header())
	require.True(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
	header := rawdb.ReadHeader(wrapper, block.Hash(), block.NumberU64())
	assertRLPEqual(t, block.Header(), header)
	require.False(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))
	require.Nil(t, rawdb.ReadBody(wrapper, block.Hash(), block.NumberU64()))

	// Verify underlying chainDB also has no header
	require.Nil(t, rawdb.ReadHeader(chainDB, block.Hash(), block.NumberU64()))

	// Write body - should persist body now
	rawdb.WriteBody(wrapper, block.Hash(), block.NumberU64(), block.Body())

	// Verify block is available
	require.True(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))
	actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
	assertRLPEqual(t, block, actualBlock)

	// Verify underlying chainDB has no header/body
	require.Nil(t, rawdb.ReadHeader(chainDB, block.Hash(), block.NumberU64()))
	require.Nil(t, rawdb.ReadBody(chainDB, block.Hash(), block.NumberU64()))
}

func TestDatabase_Batch(t *testing.T) {
	// Test that batch operations work correctly for both writing and reading block data and receipts
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	block := blocks[1]
	blockReceipts := receipts[1]

	batch := wrapper.NewBatch()

	// Write header, body, and receipts to batch
	rawdb.WriteBlock(batch, block)
	rawdb.WriteReceipts(batch, block.Hash(), block.NumberU64(), blockReceipts)

	// After writing both header and body to batch, both should be available immediately
	require.True(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))

	// Receipts should also be available immediately since they're stored separately
	require.True(t, rawdb.HasReceipts(wrapper, block.Hash(), block.NumberU64()))

	// Before write, header number should not be available
	require.Nil(t, rawdb.ReadHeaderNumber(wrapper, block.Hash()))

	// After Write(), verify header number is available
	require.NoError(t, batch.Write())
	blockNumber := rawdb.ReadHeaderNumber(wrapper, block.Hash())
	require.Equal(t, block.NumberU64(), *blockNumber)
}

func TestDatabase_SameBlockWrites(t *testing.T) {
	// Test that writing the same block twice via rawdb.WriteBlock doesn't cause issues
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, _ := createBlocks(t, 1)
	block := blocks[0]

	// Write block twice
	rawdb.WriteBlock(wrapper, block)
	rawdb.WriteBlock(wrapper, block)

	// Verify block data is still correct after duplicate writes
	require.True(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))
	actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
	assertRLPEqual(t, block, actualBlock)
}

func TestDatabase_DifferentBlocksSameHeight(t *testing.T) {
	// Test that writing different blocks to the same height overwrites the first block
	// and reading by the first block's hash returns nothing.
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	block1 := blocks[1]
	receipt1 := receipts[1]

	// Manually create a second block with the same height but different content
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	signer := types.LatestSigner(params.TestChainConfig)
	_, blocks2, receipts2, err := core.GenerateChainWithGenesis(gspec, engine, 1, 10, func(_ int, gen *core.BlockGen) {
		gen.OffsetTime(int64(5000))
		tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID:   params.TestChainConfig.ChainID,
			Nonce:     gen.TxNonce(addr1),
			To:        &addr2,
			Gas:       450000,
			GasTipCap: big.NewInt(5),
			GasFeeCap: big.NewInt(5),
		}), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(t, err)
	block2 := blocks2[0]
	receipt2 := receipts2[0]

	// Ensure both blocks have the same height but different hashes
	require.Equal(t, block1.NumberU64(), block2.NumberU64())
	require.NotEqual(t, block1.Hash(), block2.Hash())

	// Write two blocks with the same height
	writeBlocks(wrapper, []*types.Block{block1, block2}, []types.Receipts{receipt1, receipt2})

	// Reading by the first block's hash does not return anything
	require.False(t, rawdb.HasHeader(wrapper, block1.Hash(), block1.NumberU64()))
	require.False(t, rawdb.HasBody(wrapper, block1.Hash(), block1.NumberU64()))
	firstHeader := rawdb.ReadHeader(wrapper, block1.Hash(), block1.NumberU64())
	require.Nil(t, firstHeader)
	firstBody := rawdb.ReadBody(wrapper, block1.Hash(), block1.NumberU64())
	require.Nil(t, firstBody)
	require.False(t, rawdb.HasReceipts(wrapper, block1.Hash(), block1.NumberU64()))
	firstReceipts := rawdb.ReadReceipts(wrapper, block1.Hash(), block1.NumberU64(), block1.Time(), params.TestChainConfig)
	require.Nil(t, firstReceipts)

	// Reading by the second block's hash returns second block data
	require.True(t, rawdb.HasHeader(wrapper, block2.Hash(), block2.NumberU64()))
	require.True(t, rawdb.HasBody(wrapper, block2.Hash(), block2.NumberU64()))
	secondBlock := rawdb.ReadBlock(wrapper, block2.Hash(), block2.NumberU64())
	assertRLPEqual(t, block2, secondBlock)
	require.True(t, rawdb.HasReceipts(wrapper, block2.Hash(), block2.NumberU64()))
	secondReceipts := rawdb.ReadReceipts(wrapper, block2.Hash(), block2.NumberU64(), block2.Time(), params.TestChainConfig)
	assertRLPEqual(t, receipt2, secondReceipts)
}

func TestDatabase_EmptyReceipts(t *testing.T) {
	// Test that blocks with no transactions (empty receipts) are handled correctly
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)

	// Create blocks without any transactions (empty receipts)
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	_, blocks, receipts, err := core.GenerateChainWithGenesis(gspec, engine, 3, 10, func(_ int, _ *core.BlockGen) {
		// Don't add any transactions - this will create blocks with empty receipts
	})
	require.NoError(t, err)

	// Verify all blocks have empty receipts
	for i := range blocks {
		require.Empty(t, receipts[i], "Block %d should have empty receipts", i)
	}

	// write some blocks to chain db and some to wrapper and trigger migration
	writeBlocks(chainDB, blocks[:2], receipts[:2])
	writeBlocks(wrapper, blocks[2:], receipts[2:])
	require.NoError(t, wrapper.migrator.Migrate())
	waitForMigratorCompletion(t, wrapper.migrator, 10*time.Second)

	// Verify that blocks with empty receipts are handled correctly
	for _, block := range blocks {
		blockNum := block.NumberU64()

		// Block data should be accessible
		require.True(t, rawdb.HasHeader(wrapper, block.Hash(), blockNum))
		require.True(t, rawdb.HasBody(wrapper, block.Hash(), blockNum))

		// Receipts should only be stored in the receiptsDB
		require.False(t, rawdb.HasReceipts(chainDB, block.Hash(), blockNum))
		_, err := wrapper.receiptsDB.Get(blockNum)
		require.NoError(t, err)

		// to be consistent with ethdb behavior, empty receipts should return true for HasReceipts
		require.True(t, rawdb.HasReceipts(wrapper, block.Hash(), blockNum))
		recs := rawdb.ReadReceipts(wrapper, block.Hash(), blockNum, block.Time(), params.TestChainConfig)
		require.Empty(t, recs)
		logs := rawdb.ReadLogs(wrapper, block.Hash(), blockNum)
		require.Empty(t, logs)
	}
}

func TestDatabase_Close_PersistsData(t *testing.T) {
	// Test that Close() properly closes both databases and data persists
	dataDir := t.TempDir()
	wrapper, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 1)
	block := blocks[0]
	blockReceipts := receipts[0]

	// Write block and receipts
	writeBlocks(wrapper, blocks, receipts)

	// Verify data is present
	require.True(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))
	recs := rawdb.ReadReceipts(wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
	assertRLPEqual(t, blockReceipts, recs)

	// Close the wrapper db
	require.NoError(t, wrapper.Close())

	// Test we should no longer be able to read the block
	require.False(t, rawdb.HasHeader(wrapper, block.Hash(), block.NumberU64()))
	require.False(t, rawdb.HasBody(wrapper, block.Hash(), block.NumberU64()))
	_, err := wrapper.bodyDB.Get(block.NumberU64())
	require.Error(t, err)
	require.ErrorIs(t, err, database.ErrClosed)

	// Reopen the database and verify data is still present
	wrapper, _ = newDatabasesFromDir(t, dataDir)
	persistedBlock := rawdb.ReadBlock(wrapper, block.Hash(), block.NumberU64())
	assertRLPEqual(t, block, persistedBlock)
	persistedRecs := rawdb.ReadReceipts(wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
	assertRLPEqual(t, blockReceipts, persistedRecs)
	require.NoError(t, wrapper.Close())
}

func TestDatabase_ReadDuringMigration(t *testing.T) {
	// Test that blocks are readable during migration for both migrated and un-migrated blocks.
	// This test:
	// 1. Generates 21 blocks with receipts
	// 2. Adds first 20 blocks and their receipts to chainDB
	// 3. Creates wrapper database with migration disabled
	// 4. Start migration with slow block database to control migration speed
	// 5. Waits for at least 5 blocks to be migrated
	// 6. Writes block 21 during migration (this is fast)
	// 7. Verifies all 21 blocks and their receipts are readable via rawdb using wrapper

	dataDir := t.TempDir()
	// Create initial databases using newDatabasesFromDir
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 21)

	// Add first 20 blocks to KVDB (chainDB) - these will be migrated
	writeBlocks(chainDB, blocks[:20], receipts[:20])

	// Create a slow block database to control migration speed
	blockCount := 0
	slowDB := &slowDatabase{
		HeightIndex: wrapper.bodyDB,
		shouldSlow: func() bool {
			blockCount++
			return blockCount > 5 // Slow down after 5 blocks
		},
	}

	// Create and start migrator manually with slow block database
	wrapper.migrator.bodyDB = slowDB
	require.NoError(t, wrapper.migrator.Migrate())

	// Wait for at least 5 blocks to be migrated
	require.Eventually(t, func() bool {
		return wrapper.migrator.blocksProcessed() >= 5
	}, 15*time.Second, 100*time.Millisecond)

	// Write block 21 to the wrapper database (this simulates a new block being added during migration)
	writeBlocks(wrapper, blocks[20:21], receipts[20:21])

	// Verify all 21 blocks are readable via rawdb using the wrapper
	for i, block := range blocks {
		blockNum := block.NumberU64()
		expectedReceipts := receipts[i]

		// Test reading block header and body
		require.True(t, rawdb.HasHeader(wrapper, block.Hash(), blockNum))
		require.True(t, rawdb.HasBody(wrapper, block.Hash(), blockNum))
		actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), blockNum)
		require.NotNil(t, actualBlock, "Block %d should be readable", blockNum)
		assertRLPEqual(t, block, actualBlock)
		actualHeader := rawdb.ReadHeader(wrapper, block.Hash(), blockNum)
		require.NotNil(t, actualHeader, "Block %d header should be readable", blockNum)
		assertRLPEqual(t, block.Header(), actualHeader)
		actualBody := rawdb.ReadBody(wrapper, block.Hash(), blockNum)
		require.NotNil(t, actualBody, "Block %d body should be readable", blockNum)
		assertRLPEqual(t, block.Body(), actualBody)

		// Test reading receipts and logs
		actualReceipts := rawdb.ReadReceipts(wrapper, block.Hash(), blockNum, block.Time(), params.TestChainConfig)
		assertRLPEqual(t, expectedReceipts, actualReceipts)
		actualLogs := rawdb.ReadLogs(wrapper, block.Hash(), blockNum)
		expectedLogs := make([][]*types.Log, len(expectedReceipts))
		for j, receipt := range expectedReceipts {
			expectedLogs[j] = receipt.Logs
		}
		assertRLPEqual(t, expectedLogs, actualLogs)

		// Header number should be readable
		actualBlockNumber := rawdb.ReadHeaderNumber(wrapper, block.Hash())
		require.NotNil(t, actualBlockNumber, "Block %d number mapping should be readable", blockNum)
		require.Equal(t, blockNum, *actualBlockNumber)
	}

	require.NoError(t, wrapper.Close())
}

func TestDatabase_Initialization(t *testing.T) {
	blocks, _ := createBlocks(t, 10)

	testCases := []struct {
		name                string
		stateSyncEnabled    bool
		chainDBBlocks       []*types.Block
		existingDBMinHeight *uint64
		expInitialized      bool
		expMinHeight        uint64
		expMinHeightSet     bool
	}{
		{
			name:             "empty_chainDB_no_state_sync",
			stateSyncEnabled: false,
			expInitialized:   true,
			expMinHeight:     1,
			expMinHeightSet:  true,
		},
		{
			name:             "empty_chainDB_state_sync_no_init",
			stateSyncEnabled: true,
			expInitialized:   false,
			expMinHeight:     0,
			expMinHeightSet:  false,
		},
		{
			name:             "migration_needed",
			stateSyncEnabled: false,
			chainDBBlocks:    blocks[5:10],
			expInitialized:   true,
			expMinHeight:     5,
			expMinHeightSet:  true,
		},
		{
			name:             "migration_needed_with_genesis",
			stateSyncEnabled: false,
			chainDBBlocks:    append([]*types.Block{blocks[0]}, blocks[5:10]...),
			expInitialized:   true,
			expMinHeight:     5,
			expMinHeightSet:  true,
		},
		{
			name:             "migration_needed_state_sync",
			stateSyncEnabled: true,
			chainDBBlocks:    blocks[5:10],
			expInitialized:   true,
			expMinHeight:     5,
			expMinHeightSet:  true,
		},
		{
			name:                "existing_db_created_with_min_height",
			existingDBMinHeight: func() *uint64 { v := uint64(2); return &v }(),
			stateSyncEnabled:    false,
			chainDBBlocks:       blocks[5:8],
			expInitialized:      true,
			expMinHeight:        2,
			expMinHeightSet:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
			require.NoError(t, err)
			chainDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))
			blockDBPath := filepath.Join(dataDir, "blockdb")

			// Create the block database with an existing min height if needed
			if tc.existingDBMinHeight != nil {
				minHeight, err := getDatabaseMinHeight(base)
				require.NoError(t, err)
				require.Nil(t, minHeight)
				wrapper := New(
					base,
					chainDB,
					blockdb.DefaultConfig(),
					blockDBPath,
					logging.NoLog{},
					prometheus.NewRegistry(),
				)
				require.NoError(t, wrapper.InitWithMinHeight(*tc.existingDBMinHeight))
				require.NoError(t, wrapper.bodyDB.Close())
				require.NoError(t, wrapper.receiptsDB.Close())
				minHeight, err = getDatabaseMinHeight(base)
				require.NoError(t, err)
				require.Equal(t, *tc.existingDBMinHeight, *minHeight)
			}

			// write chainDB blocks if needed
			if len(tc.chainDBBlocks) > 0 {
				writeBlocks(chainDB, tc.chainDBBlocks, []types.Receipts{})
			}

			// Create wrapper database
			wrapper := New(
				base,
				chainDB,
				blockdb.DefaultConfig(),
				blockDBPath,
				logging.NoLog{},
				prometheus.NewRegistry(),
			)
			initialized, err := wrapper.InitWithStateSync(tc.stateSyncEnabled)
			require.NoError(t, err)
			require.Equal(t, tc.expInitialized, initialized)
			require.Equal(t, tc.expInitialized, wrapper.initialized)

			// Verify initialization state and min height
			if tc.expMinHeightSet {
				require.Equal(t, tc.expMinHeight, wrapper.minHeight)
			} else {
				require.Equal(t, uint64(0), wrapper.minHeight)
			}

			require.NoError(t, wrapper.Close())
		})
	}
}

// Test that genesis block (block number 0) behavior works correctly.
// Genesis blocks should only exist in chainDB and not in the wrapper's block database.
func TestDatabase_Genesis(t *testing.T) {
	dataDir := t.TempDir()
	wrapper, chainDB := newDatabasesFromDir(t, dataDir)
	require.True(t, wrapper.initialized)
	require.Equal(t, uint64(1), wrapper.minHeight)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(wrapper, blocks, receipts)

	// validate genesis block can be retrieved and its stored in chainDB
	genesisHash := rawdb.ReadCanonicalHash(chainDB, 0)
	genesisBlock := rawdb.ReadBlock(wrapper, genesisHash, 0)
	assertRLPEqual(t, blocks[0], genesisBlock)
	has, _ := wrapper.bodyDB.Has(0)
	require.False(t, has)
	require.Equal(t, uint64(1), wrapper.minHeight)
}

func assertRLPEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()

	expectedBytes, err := rlp.EncodeToBytes(expected)
	require.NoError(t, err)
	actualBytes, err := rlp.EncodeToBytes(actual)
	require.NoError(t, err)
	require.Equal(t, expectedBytes, actualBytes)
}
