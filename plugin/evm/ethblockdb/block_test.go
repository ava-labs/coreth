// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethblockdb

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/coreth/internal/blocktest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
)

// createTestBlock creates a test block with transactions
func createTestBlock() *types.Block {
	// Create test header
	header := &types.Header{
		ParentHash: common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Difficulty: big.NewInt(0x020000),
		Number:     big.NewInt(1),
		GasLimit:   0x2fefd8,
		Time:       0x55ba467c,
		Extra:      []byte("test block extra data"),
	}

	// Create test transactions
	tx1 := types.NewTransaction(1, common.BytesToAddress([]byte{0x11}), big.NewInt(111), 1111, big.NewInt(11111), []byte{0x11, 0x11, 0x11})
	tx2 := types.NewTransaction(2, common.BytesToAddress([]byte{0x22}), big.NewInt(222), 2222, big.NewInt(22222), []byte{0x22, 0x22, 0x22})
	tx3 := types.NewTransaction(3, common.BytesToAddress([]byte{0x33}), big.NewInt(333), 3333, big.NewInt(33333), []byte{0x33, 0x33, 0x33})
	txs := []*types.Transaction{tx1, tx2, tx3}

	// Create block
	block := types.NewBlock(header, txs, nil, nil, blocktest.NewHasher())
	return block
}

func newDatabase(t *testing.T) Database {
	dir := t.TempDir()
	db, err := blockdb.New(blockdb.DefaultConfig().WithDir(dir), logging.NoLog{})
	require.NoError(t, err)
	chainDb := rawdb.NewMemoryDatabase()
	return New(db, chainDb)
}

func checkHeader(t *testing.T, originalBlock *types.Block, readHeader *types.Header) {
	require.Equal(t, originalBlock.Header().ParentHash, readHeader.ParentHash)
	require.Equal(t, originalBlock.Header().Number, readHeader.Number)
	require.Equal(t, originalBlock.Header().GasLimit, readHeader.GasLimit)
	require.Equal(t, originalBlock.Header().Time, readHeader.Time)
	require.Equal(t, originalBlock.Header().Extra, readHeader.Extra)
	require.Equal(t, originalBlock.Header().Difficulty, readHeader.Difficulty)
	require.Equal(t, originalBlock.Header().Coinbase, readHeader.Coinbase)
}

func checkBody(t *testing.T, originalBlock *types.Block, readBody *types.Body) {
	require.Equal(t, len(originalBlock.Transactions()), len(readBody.Transactions))
	require.Equal(t, len(originalBlock.Uncles()), len(readBody.Uncles))

	// Verify transaction hashes match
	for i, tx := range originalBlock.Transactions() {
		require.Equal(t, tx.Hash(), readBody.Transactions[i].Hash())
	}
}

func TestWriteBlock(t *testing.T) {
	db := newDatabase(t)
	block := createTestBlock()
	db.WriteBlock(block)
	readBlock := db.ReadBlock(block.NumberU64())
	require.NotNil(t, readBlock)
}

func TestReadHeader(t *testing.T) {
	db := newDatabase(t)
	originalBlock := createTestBlock()

	// Write block
	db.WriteBlock(originalBlock)

	// read and check header
	readHeader := db.ReadHeader(1)
	require.NotNil(t, readHeader)
	checkHeader(t, originalBlock, readHeader)
}

func TestReadBody(t *testing.T) {
	db := newDatabase(t)
	originalBlock := createTestBlock()

	// Write block
	db.WriteBlock(originalBlock)

	// read and check body
	readBody := db.ReadBody(1)
	require.NotNil(t, readBody)
	checkBody(t, originalBlock, readBody)
}

func TestReadBlock(t *testing.T) {
	db := newDatabase(t)
	originalBlock := createTestBlock()

	// Write block
	db.WriteBlock(originalBlock)

	// read and check block
	readBlock := db.ReadBlock(1)
	require.NotNil(t, readBlock)
	checkBody(t, originalBlock, readBlock.Body())
	checkHeader(t, originalBlock, readBlock.Header())
}

func TestHasBlock(t *testing.T) {
	db := newDatabase(t)
	originalBlock := createTestBlock()

	// Write block
	db.WriteBlock(originalBlock)

	hasBlock := db.HasBlock(1)
	require.True(t, hasBlock)

	hasBlock = db.HasBlock(2)
	require.False(t, hasBlock)

	hasBlock = db.HasBlock(math.MaxUint64)
	require.False(t, hasBlock)
}
