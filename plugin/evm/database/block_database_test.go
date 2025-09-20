// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package database

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
)

type setupCfg struct {
	numBlocks int
	migration migrationStatus
	useDiskDB bool
}

type testEnv struct {
	wrapper  *wrappedBlockDatabase
	chaindb  ethdb.Database
	blockdb  database.BlockDatabase
	blocks   []*types.Block
	receipts []types.Receipts
}

var (
	key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	addr1   = crypto.PubkeyToAddress(key1.PublicKey)
	addr2   = crypto.PubkeyToAddress(key2.PublicKey)
)

func setup(t *testing.T, cfg setupCfg) *testEnv {
	t.Helper()

	dataDir := t.TempDir()

	var base database.Database
	var chaindb ethdb.Database
	var bdb database.BlockDatabase
	if cfg.useDiskDB {
		base, chaindb, bdb = newDatabasesFromDir(t, dataDir)
	} else {
		base = memdb.New()
		chaindb = rawdb.NewDatabase(WrapDatabase(base))
		bdb = blockdb.NewMemoryDatabase()
	}

	// configure migration state
	require.NoError(t, chaindb.Put(migrationStatusKey, []byte{byte(cfg.migration)}))

	// wrapper under test
	w, err := NewWrappedBlockDatabase(base, bdb, chaindb, true)
	require.NoError(t, err)

	blocks, receipts := createBlocks(t, cfg.numBlocks)
	return &testEnv{
		wrapper:  w,
		chaindb:  chaindb,
		blockdb:  bdb,
		blocks:   blocks,
		receipts: receipts,
	}
}

func createBlocks(t *testing.T, numBlocks int) ([]*types.Block, []types.Receipts) {
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	signer := types.LatestSigner(params.TestChainConfig)
	_, blocks, receipts, err := core.GenerateChainWithGenesis(gspec, engine, numBlocks, 10, func(_ int, gen *core.BlockGen) {
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
	return blocks, receipts
}

func TestWrappedBlockDatabase_BlockOperations(t *testing.T) {
	// Test wrappedBlockDatabase by using it to write, read, and delete blocks and receipts.
	// We are verifying that block header and body is stored in the blockdb and other
	// block data is stored in the ethdb and that the read operations are consistent.
	// We are also verifying that the block data is deleted correctly.

	env := setup(t, setupCfg{numBlocks: 3, migration: migrationStatusCompleted})

	// write all blocks and receipts
	for i := 0; i < len(env.blocks); i++ {
		block := env.blocks[i]
		receipts := env.receipts[i]
		rawdb.WriteBlock(env.wrapper, block)
		rawdb.WriteReceipts(env.wrapper, block.Hash(), block.NumberU64(), receipts)
	}

	// read all blocks and receipts
	for i, block := range env.blocks {
		require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
		require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))

		// Read non block header and body data
		expectedReceipts := env.receipts[i]
		recs := rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		assertRLPEqual(t, expectedReceipts, recs)
		logs := rawdb.ReadLogs(env.wrapper, block.Hash(), block.NumberU64())
		expectedLogs := make([][]*types.Log, len(expectedReceipts))
		for i, receipt := range expectedReceipts {
			expectedLogs[i] = receipt.Logs
		}
		assertRLPEqual(t, expectedLogs, logs)
		blockNumber := rawdb.ReadHeaderNumber(env.wrapper, block.Hash())
		require.Equal(t, block.NumberU64(), *blockNumber)

		// Read blockdb data
		header := rawdb.ReadHeader(env.wrapper, block.Hash(), block.NumberU64())
		require.Equal(t, header, block.Header())
		body := rawdb.ReadBody(env.wrapper, block.Hash(), block.NumberU64())
		assertRLPEqual(t, block.Body(), body)

		// ensure that the underlying eth database does not store block header and body
		require.Nil(t, rawdb.ReadHeader(env.chaindb, block.Hash(), block.NumberU64()))
		require.Nil(t, rawdb.ReadBody(env.chaindb, block.Hash(), block.NumberU64()))
		require.False(t, rawdb.HasHeader(env.chaindb, block.Hash(), block.NumberU64()))
		require.False(t, rawdb.HasBody(env.chaindb, block.Hash(), block.NumberU64()))
	}

	// delete block data
	for _, block := range env.blocks {
		rawdb.DeleteBlock(env.wrapper, block.Hash(), block.NumberU64())

		// we cannot delete header or body
		require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
		require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))
		header := rawdb.ReadHeader(env.wrapper, block.Hash(), block.NumberU64())
		require.Equal(t, block.Header(), header)
		body := rawdb.ReadBody(env.wrapper, block.Hash(), block.NumberU64())
		assertRLPEqual(t, block.Body(), body)

		// hash to height mapping should be deleted
		blockNumber := rawdb.ReadHeaderNumber(env.wrapper, block.Hash())
		require.Nil(t, blockNumber)

		// Receipts and logs should be deleted
		recs := rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		assertRLPEqual(t, types.Receipts{}, recs)
		logs := rawdb.ReadLogs(env.wrapper, block.Hash(), block.NumberU64())
		assertRLPEqual(t, [][]*types.Log{}, logs)
	}
}

func TestWrappedBlockDatabase_WriteOrder_CachesUntilBoth(t *testing.T) {
	// Test that partial writes (header or body only) are cached and not persisted
	// until both header and body are available, then written as a single RLP block
	env := setup(t, setupCfg{numBlocks: 1, migration: migrationStatusCompleted})
	block := env.blocks[0]

	// Write header first - should be cached but not persisted
	rawdb.WriteHeader(env.wrapper, block.Header())

	// Verify header is not yet available (cached but not written to blockdb)
	require.False(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
	require.Nil(t, rawdb.ReadHeader(env.wrapper, block.Hash(), block.NumberU64()))

	// Verify underlying kvdb also has no header
	require.Nil(t, rawdb.ReadHeader(env.chaindb, block.Hash(), block.NumberU64()))

	// Write body - should trigger writing both header and body to blockdb
	rawdb.WriteBody(env.wrapper, block.Hash(), block.NumberU64(), block.Body())

	// Now both should be available
	require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))
	actualBlock := rawdb.ReadBlock(env.wrapper, block.Hash(), block.NumberU64())
	assertRLPEqual(t, block, actualBlock)

	// Underlying kvdb should still have no header/body
	require.Nil(t, rawdb.ReadHeader(env.chaindb, block.Hash(), block.NumberU64()))
	require.Nil(t, rawdb.ReadBody(env.chaindb, block.Hash(), block.NumberU64()))
}

func TestWrappedBlockDatabase_Batch_WritesAndReads(t *testing.T) {
	// Test that batch operations work correctly for both block data and non-block data
	env := setup(t, setupCfg{numBlocks: 1, migration: migrationStatusCompleted})
	block := env.blocks[0]
	receipts := env.receipts[0]

	batch := env.wrapper.NewBatch()

	// Write header, body, and receipts to batch
	rawdb.WriteBlock(batch, block)
	rawdb.WriteReceipts(batch, block.Hash(), block.NumberU64(), receipts)

	// After writing both header and body to batch, both should be available immediately
	// because the second write triggers the cache mechanism and writes to blockdb
	require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))

	// Receipts should not be available before Write()
	recs := rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
	require.Empty(t, recs)

	// After Write(), receipts and block should be available
	require.NoError(t, batch.Write())
	assertRLPEqual(t, block, rawdb.ReadBlock(env.wrapper, block.Hash(), block.NumberU64()))
	recs = rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
	assertRLPEqual(t, receipts, recs)
}

func TestWrappedBlockDatabase_DuplicateWrites(t *testing.T) {
	// Test that writing the same block twice via rawdb.WriteBlock doesn't cause issues
	// but the original block data should be overwritten
	env := setup(t, setupCfg{numBlocks: 1, migration: migrationStatusCompleted})
	block := env.blocks[0]

	// Write block twice
	rawdb.WriteBlock(env.wrapper, block)
	rawdb.WriteBlock(env.wrapper, block)

	// Verify block data is still correct after duplicate writes
	require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))

	actualBlock := rawdb.ReadBlock(env.wrapper, block.Hash(), block.NumberU64())
	assertRLPEqual(t, block, actualBlock)
}

func TestWrappedBlockDatabase_SameHeightWrites(t *testing.T) {
	// Test that writing different blocks to the same height overwrites the first block
	// and reading by the first block's hash returns the second block (latest block at that height)

	// Use setup to generate the first block
	env := setup(t, setupCfg{numBlocks: 1, migration: migrationStatusCompleted})
	block1 := env.blocks[0]

	// Manually create a second block with the same height but different content
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	signer := types.LatestSigner(params.TestChainConfig)
	_, blocks2, _, err := core.GenerateChainWithGenesis(gspec, engine, 1, 10, func(_ int, gen *core.BlockGen) {
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

	// Ensure both blocks have the same height but different hashes
	require.Equal(t, block1.NumberU64(), block2.NumberU64())
	require.NotEqual(t, block1.Hash(), block2.Hash())

	// Write first block and verify it is available
	rawdb.WriteBlock(env.wrapper, block1)
	actualBlock1 := rawdb.ReadBlock(env.wrapper, block1.Hash(), block1.NumberU64())
	assertRLPEqual(t, block1, actualBlock1)

	// Write second block and verify it is available
	rawdb.WriteBlock(env.wrapper, block2)
	actualBlock2 := rawdb.ReadBlock(env.wrapper, block2.Hash(), block2.NumberU64())
	assertRLPEqual(t, block2, actualBlock2)

	// Reading by the first block's hash does not return anything
	canonicalBlock := rawdb.ReadBlock(env.wrapper, block1.Hash(), block1.NumberU64())
	require.Nil(t, canonicalBlock)
}

func TestWrappedBlockDatabase_Delete_BlockKeysNoop(t *testing.T) {
	// Test that delete operations on block header/body are no-ops
	env := setup(t, setupCfg{numBlocks: 1, migration: migrationStatusCompleted})
	block := env.blocks[0]
	receipts := env.receipts[0]

	// Write block and receipts
	rawdb.WriteBlock(env.wrapper, block)
	rawdb.WriteReceipts(env.wrapper, block.Hash(), block.NumberU64(), receipts)

	// Delete block data - should be no-op
	rawdb.DeleteBlock(env.wrapper, block.Hash(), block.NumberU64())

	// Block header and body should still be present (no-op for blockdb)
	require.Equal(t, block.Header(), rawdb.ReadHeader(env.wrapper, block.Hash(), block.NumberU64()))
	assertRLPEqual(t, block.Body(), rawdb.ReadBody(env.wrapper, block.Hash(), block.NumberU64()))

	// Non-block data should be deleted
	blockNumber := rawdb.ReadHeaderNumber(env.wrapper, block.Hash())
	require.Nil(t, blockNumber)
	require.Empty(t, rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig))
}

func TestWrappedBlockDatabase_Close_PersistsData(t *testing.T) {
	// Test that Close() properly closes both databases and data persists
	env := setup(t, setupCfg{numBlocks: 1, migration: migrationStatusCompleted, useDiskDB: true})
	block := env.blocks[0]
	receipts := env.receipts[0]

	// Write block and receipts
	rawdb.WriteBlock(env.wrapper, block)
	rawdb.WriteReceipts(env.wrapper, block.Hash(), block.NumberU64(), receipts)

	// Verify data is present
	require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))
	recs := rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
	assertRLPEqual(t, receipts, recs)

	// Close the wrapper db
	require.NoError(t, env.wrapper.Close())

	// Test we should no longer be able to read the block
	_, err := env.blockdb.ReadBlock(block.NumberU64())
	require.Error(t, err)
}

func TestWrappedBlockDatabase_MigrationReadability(t *testing.T) {
	// Test that blocks are readable during migration for both migrated and unmigrated blocks.
	// This test:
	// 1. Generates 21 blocks with receipts
	// 2. Adds first 20 blocks and their receipts to KVDB (chaindb)
	// 3. Creates wrapper database with slow migration to trigger migration
	// 4. Waits for 5 blocks to be migrated
	// 5. Replaces blockdb with non-slow DB and writes block 21 with receipts
	// 6. Verifies all 21 blocks and their receipts are readable via rawdb using wrapper

	dataDir := t.TempDir()
	base, chaindb, bdb := newDatabasesFromDir(t, dataDir)

	// Generate 21 blocks
	blocks, receipts := createBlocks(t, 21)
	require.Len(t, blocks, 21)

	// Add first 20 blocks to KVDB (chaindb) - these will be migrated
	writeKVDBBlocks(chaindb, blocks[:20])
	for i := 0; i < 20; i++ {
		rawdb.WriteReceipts(chaindb, blocks[i].Hash(), blocks[i].NumberU64(), receipts[i])
	}

	// Create a slow block database that allows us to control migration speed
	// It will slow down after 5 blocks to simulate partial migration
	blockCount := 0
	slowBdb := &slowBlockDatabase{
		BlockDatabase: bdb,
		shouldSlow: func() bool {
			blockCount++
			return blockCount > 5 // Slow down after 5 blocks
		},
	}

	// Create wrapper with slow migration - this should trigger migration
	wrapper, err := NewWrappedBlockDatabase(base, slowBdb, chaindb, true)
	require.NoError(t, err)

	// Replace the wrapper's blockdb instance with the non-slow DB
	wrapper.blockdb = bdb

	// Write block 21 to the wrapper database (this simulates a new block being added during migration)
	rawdb.WriteBlock(wrapper, blocks[20])
	rawdb.WriteReceipts(wrapper, blocks[20].Hash(), blocks[20].NumberU64(), receipts[20])

	// Wait for exactly 5 blocks to be migrated
	require.Eventually(t, func() bool {
		return wrapper.migrator.getBlocksProcessed() >= 5
	}, 15*time.Second, 100*time.Millisecond)

	// Verify all 21 blocks are readable via rawdb using the wrapper
	for i, block := range blocks {
		blockNum := block.NumberU64()
		expectedReceipts := receipts[i]

		// Test block header and body readability
		require.True(t, rawdb.HasHeader(wrapper, block.Hash(), blockNum),
			"Block %d header should be readable", blockNum)
		require.True(t, rawdb.HasBody(wrapper, block.Hash(), blockNum),
			"Block %d body should be readable", blockNum)

		// Test reading the complete block
		actualBlock := rawdb.ReadBlock(wrapper, block.Hash(), blockNum)
		require.NotNil(t, actualBlock, "Block %d should be readable", blockNum)
		assertRLPEqual(t, block, actualBlock)

		// Test reading header and body separately
		actualHeader := rawdb.ReadHeader(wrapper, block.Hash(), blockNum)
		require.NotNil(t, actualHeader, "Block %d header should be readable", blockNum)
		assertRLPEqual(t, block.Header(), actualHeader)

		actualBody := rawdb.ReadBody(wrapper, block.Hash(), blockNum)
		require.NotNil(t, actualBody, "Block %d body should be readable", blockNum)
		assertRLPEqual(t, block.Body(), actualBody)

		// Test reading receipts for all blocks
		actualReceipts := rawdb.ReadReceipts(wrapper, block.Hash(), blockNum, block.Time(), params.TestChainConfig)
		assertRLPEqual(t, expectedReceipts, actualReceipts)

		// Test reading logs for all blocks
		actualLogs := rawdb.ReadLogs(wrapper, block.Hash(), blockNum)
		expectedLogs := make([][]*types.Log, len(expectedReceipts))
		for j, receipt := range expectedReceipts {
			expectedLogs[j] = receipt.Logs
		}
		assertRLPEqual(t, expectedLogs, actualLogs)

		// Test hash to number mapping
		actualBlockNumber := rawdb.ReadHeaderNumber(wrapper, block.Hash())
		require.NotNil(t, actualBlockNumber, "Block %d number mapping should be readable", blockNum)
		require.Equal(t, blockNum, *actualBlockNumber)
	}

	require.NoError(t, wrapper.Close())
}

func TestWrappedBlockDatabase_HashMismatch(t *testing.T) {
	// Test that reading blocks with incorrect hashes returns nothing
	env := setup(t, setupCfg{numBlocks: 10, migration: migrationStatusCompleted})

	// Write all blocks
	for i := 0; i < len(env.blocks); i++ {
		block := env.blocks[i]
		receipts := env.receipts[i]
		rawdb.WriteBlock(env.wrapper, block)
		rawdb.WriteReceipts(env.wrapper, block.Hash(), block.NumberU64(), receipts)
	}

	// Test reading blocks with correct hashes should work
	for i, block := range env.blocks {
		require.True(t, rawdb.HasHeader(env.wrapper, block.Hash(), block.NumberU64()))
		require.True(t, rawdb.HasBody(env.wrapper, block.Hash(), block.NumberU64()))
		assertRLPEqual(t, block.Header(), rawdb.ReadHeader(env.wrapper, block.Hash(), block.NumberU64()))
		assertRLPEqual(t, block.Body(), rawdb.ReadBody(env.wrapper, block.Hash(), block.NumberU64()))
		assertRLPEqual(t, block, rawdb.ReadBlock(env.wrapper, block.Hash(), block.NumberU64()))
		assertRLPEqual(t, env.receipts[i], rawdb.ReadReceipts(env.wrapper, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig))

		// Test reading with invalid hash - should return nothing
		nextIndex := (i + 1) % len(env.blocks)
		invalidHash := env.blocks[nextIndex].Hash() // Use next block's hash, wrap around to 0
		require.False(t, rawdb.HasHeader(env.wrapper, invalidHash, block.NumberU64()))
		require.False(t, rawdb.HasBody(env.wrapper, invalidHash, block.NumberU64()))
		require.Nil(t, rawdb.ReadHeader(env.wrapper, invalidHash, block.NumberU64()))
		require.Nil(t, rawdb.ReadBody(env.wrapper, invalidHash, block.NumberU64()))
		require.Nil(t, rawdb.ReadBlock(env.wrapper, invalidHash, block.NumberU64()))
	}
}

func assertRLPEqual(t *testing.T, expected, actual interface{}) {
	expectedBytes, err := rlp.EncodeToBytes(expected)
	require.NoError(t, err)
	actualBytes, err := rlp.EncodeToBytes(actual)
	require.NoError(t, err)
	require.Equal(t, expectedBytes, actualBytes)
}
