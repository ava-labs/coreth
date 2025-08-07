// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethblockdb

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/coreth/internal/blocktest"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/database"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// calculateBlockSize calculates the size of a block using the same approach as ethblockdb
func calculateBlockSize(block *types.Block) int {
	header := block.Header()
	body := block.Body()

	// RLP encode header and body like in ethblockdb
	rlpHeader, err := rlp.EncodeToBytes(header)
	if err != nil {
		return 0
	}
	headerSize := len(rlpHeader)

	rlpBody, err := rlp.EncodeToBytes(body)
	if err != nil {
		return 0
	}
	bodySize := len(rlpBody)

	// Total size is header + body (same as in ethblockdb WriteBlock)
	return headerSize + bodySize
}

// generateSingleBlock creates a single block at the specified height
func generateSingleBlock(height uint64, parentHash common.Hash) *types.Block {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr2   = crypto.PubkeyToAddress(key1.PublicKey)
		signer  = types.HomesteadSigner{}
	)

	// Create header
	header := &types.Header{
		ParentHash: parentHash,
		Number:     big.NewInt(int64(height)),
		Difficulty: big.NewInt(1),
		GasLimit:   0x2fefd8,
		Time:       uint64(0x55ba467c + height),
		Extra:      []byte("test block"),
	}

	// Create transaction
	tx, _ := types.SignTx(types.NewTransaction(height, addr2, big.NewInt(10), params.TxGas, nil, nil), signer, key1)
	txs := []*types.Transaction{tx}

	// Create block
	block := types.NewBlock(header, txs, nil, nil, blocktest.NewHasher())
	return block
}

// generateTestBlocks creates a chain of blocks with transactions for benchmarking
func generateTestBlocks(b *testing.B, numBlocks int) []*types.Block {
	b.Helper()

	blocks := make([]*types.Block, numBlocks)

	// Generate blocks with transactions starting from height 1
	parentHash := crypto.Keccak256Hash([]byte("parent"))
	for i := 0; i < numBlocks; i++ {
		block := generateSingleBlock(uint64(i), parentHash)
		blocks[i] = block
		parentHash = block.Hash()
	}

	// Log average block size
	avgSize := float64(calculateBlockSize(blocks[0]))
	b.Logf("Generated %d blocks starting from height 0, average size: %.2f bytes", numBlocks, avgSize)

	return blocks
}

// createRawDB creates a leveldb database on disk
func createRawDB(b *testing.B, dir string) ethdb.Database {
	b.Helper()

	db, err := leveldb.New(dir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(b, err)
	chainDb := rawdb.NewDatabase(database.WrapDatabase(db))
	return chainDb
}

// createBlockDB creates a blockdb database on disk
func createBlockDB(b *testing.B, dir string, syncToDisk bool, compressBlocks bool) (ethdb.Database, Database) {
	b.Helper()

	// Create blockdb
	chainDB := createRawDB(b, dir)
	store, err := blockdb.New(blockdb.DefaultConfig().WithDir(dir).WithSyncToDisk(syncToDisk).WithCompressBlocks(compressBlocks), logging.NoLog{})
	require.NoError(b, err)

	blockDB := New(store, chainDB)
	return chainDB, blockDB
}

func BenchmarkWriteBlock_10k(b *testing.B) {
	// Generate blocks once before benchmarking to avoid counting generation time
	blocks := generateTestBlocks(b, 10_000)

	b.Run("BlockDB-SyncOff-NoCompression", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Stop timer before database creation to exclude creation time
			b.StopTimer()

			// Create fresh databases for each iteration
			chainDB, blockDB := createBlockDB(b, b.TempDir(), false, false)
			defer chainDB.Close()

			// Start timer after database creation
			b.StartTimer()

			for _, block := range blocks {
				blockDB.WriteBlock(block)
			}
		}
	})

	b.Run("BlockDB-SyncOff-Compression", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Stop timer before database creation to exclude creation time
			b.StopTimer()

			// Create fresh databases for each iteration
			chainDB, blockDB := createBlockDB(b, b.TempDir(), false, true)
			defer chainDB.Close()

			// Start timer after database creation
			b.StartTimer()

			for _, block := range blocks {
				blockDB.WriteBlock(block)
			}
		}
	})

	b.Run("BlockDB-SyncOn-Compression", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Stop timer before database creation to exclude creation time
			b.StopTimer()

			// Create fresh databases for each iteration with sync enabled
			chainDB, blockDB := createBlockDB(b, b.TempDir(), true, true)
			defer chainDB.Close()

			// Start timer after database creation
			b.StartTimer()

			for _, block := range blocks {
				blockDB.WriteBlock(block)
			}
		}
	})

	b.Run("RawDB-NoCompact", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Stop timer before database creation to exclude creation time
			b.StopTimer()

			// Create fresh database for each iteration
			db := createRawDB(b, b.TempDir())
			defer db.Close()

			// Start timer after database creation
			b.StartTimer()

			for _, block := range blocks {
				rawdb.WriteBlock(db, block)
			}
		}
	})

	b.Run("RawDB-Compact", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Stop timer before database creation to exclude creation time
			b.StopTimer()

			// Create fresh database for each iteration
			db := createRawDB(b, b.TempDir())
			defer db.Close()

			// Start timer after database creation
			b.StartTimer()

			for _, block := range blocks {
				rawdb.WriteBlock(db, block)
			}
			db.Compact(nil, nil)
		}
	})
}

func BenchmarkReadBlock_1k(b *testing.B) {
	// Generate blocks once before benchmarking to avoid counting generation time
	blocks := generateTestBlocks(b, 1000)

	b.Run("RawDB", func(b *testing.B) {
		// Pre-populate database
		db := createRawDB(b, b.TempDir())
		defer db.Close()

		batch := db.NewBatch()
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
		}
		if err := batch.Write(); err != nil {
			b.Fatalf("Failed to write batch: %v", err)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Read a random block from height 0-1000
			randomHeight := uint64(i % 1000) // Use i to get different heights across iterations
			readBlock := rawdb.ReadBlock(db, blocks[randomHeight].Hash(), randomHeight)
			if readBlock == nil {
				b.Fatalf("Failed to read block %d", randomHeight)
			}
		}
	})

	b.Run("RawDB-Concurrent", func(b *testing.B) {
		// Pre-populate database
		db := createRawDB(b, b.TempDir())
		defer db.Close()

		batch := db.NewBatch()
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
		}
		if err := batch.Write(); err != nil {
			b.Fatalf("Failed to write batch: %v", err)
		}

		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				// Read a random block from height 0-1000
				randomHeight := uint64(i % 1000)
				readBlock := rawdb.ReadBlock(db, blocks[randomHeight].Hash(), randomHeight)
				if readBlock == nil {
					b.Errorf("Failed to read block %d", randomHeight)
				}
				i++
			}
		})
	})

	b.Run("BlockDB", func(b *testing.B) {
		// Pre-populate database
		chainDB, blockDB := createBlockDB(b, b.TempDir(), false, false)
		defer chainDB.Close()

		for _, block := range blocks {
			blockDB.WriteBlock(block)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Read a random block from height 0-1000
			randomHeight := uint64(i % 1000) // Use i to get different heights across iterations
			readBlock := blockDB.ReadBlock(randomHeight)
			if readBlock == nil {
				b.Fatalf("Failed to read block %d", randomHeight)
			}
		}
	})

	b.Run("BlockDB-Concurrent", func(b *testing.B) {
		// Pre-populate database
		chainDB, blockDB := createBlockDB(b, b.TempDir(), false, false)
		defer chainDB.Close()

		for _, block := range blocks {
			blockDB.WriteBlock(block)
		}

		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				// Read a random block from height 0-1000
				randomHeight := uint64(i % 1000)
				readBlock := blockDB.ReadBlock(randomHeight)
				if readBlock == nil {
					b.Errorf("Failed to read block %d", randomHeight)
				}
				i++
			}
		})
	})
}

func TestDirectorySizeComparison(t *testing.T) {
	const numBlocks = 500_000

	// Test RawDB
	t.Run("RawDB", func(t *testing.T) {
		dir := t.TempDir()
		db := createRawDBT(t, dir)
		defer db.Close()

		// Generate blocks
		blocks := generateTestBlocksT(t, numBlocks)

		// Write blocks
		batch := db.NewBatch()
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
		}
		if err := batch.Write(); err != nil {
			t.Fatalf("Failed to write batch: %v", err)
		}

		// Compact to flush everything
		if err := db.Compact(nil, nil); err != nil {
			t.Fatalf("Failed to compact database: %v", err)
		}

		// Calculate directory size
		size, err := getDirectorySize(dir)
		if err != nil {
			t.Fatalf("Failed to get directory size: %v", err)
		}

		t.Logf("RawDB: Wrote %d blocks, directory size: %s (%.2f bytes per block)",
			numBlocks, formatBytes(size), float64(size)/float64(numBlocks))
	})

	// Test BlockDB with compression
	t.Run("BlockDB-Compression", func(t *testing.T) {
		dir := t.TempDir()
		chainDB, blockDB := createBlockDBT(t, dir, false, true)
		defer chainDB.Close()

		// Generate blocks
		blocks := generateTestBlocksT(t, numBlocks)

		// Write blocks
		for _, block := range blocks {
			blockDB.WriteBlock(block)
		}

		// Calculate directory size
		size, err := getDirectorySize(dir)
		if err != nil {
			t.Fatalf("Failed to get directory size: %v", err)
		}

		t.Logf("BlockDB-Compression: Wrote %d blocks, directory size: %s (%.2f bytes per block)",
			numBlocks, formatBytes(size), float64(size)/float64(numBlocks))
	})

	// Test BlockDB without compression
	t.Run("BlockDB-NoCompression", func(t *testing.T) {
		dir := t.TempDir()
		chainDB, blockDB := createBlockDBT(t, dir, false, false)
		defer chainDB.Close()

		// Generate blocks
		blocks := generateTestBlocksT(t, numBlocks)

		// Write blocks
		for _, block := range blocks {
			blockDB.WriteBlock(block)
		}

		// Calculate directory size
		size, err := getDirectorySize(dir)
		if err != nil {
			t.Fatalf("Failed to get directory size: %v", err)
		}

		t.Logf("BlockDB-NoCompression: Wrote %d blocks, directory size: %s (%.2f bytes per block)",
			numBlocks, formatBytes(size), float64(size)/float64(numBlocks))
	})
}

// getDirectorySize calculates the total size of a directory in bytes
func getDirectorySize(dir string) (int64, error) {
	var size int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
