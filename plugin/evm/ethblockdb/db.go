// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethblockdb

import (
	"math/big"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/coreth/consensus/misc/eip4844"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
)

type Database interface {
	ReadBody(height uint64) *types.Body
	ReadHeader(height uint64) *types.Header
	ReadBlock(height uint64) *types.Block
	HasBlock(height uint64) bool
	WriteBlock(block *types.Block)
	ReadReceipts(hash common.Hash, number uint64, time uint64, config *params.ChainConfig) types.Receipts
	ReadTransaction(hash common.Hash) (*types.Transaction, common.Hash, uint64, uint64)
}

type db struct {
	database.BlockDatabase
	chainDb ethdb.Database
}

func New(database database.BlockDatabase, chainDb ethdb.Database) *db {
	return &db{BlockDatabase: database, chainDb: chainDb}
}

func NewMock(chainDb ethdb.Database) *db {
	return New(blockdb.NewMock(), chainDb)
}

func (d *db) WriteBlock(block *types.Block) {
	header := block.Header()
	body := block.Body()

	rlpHeader, err := rlp.EncodeToBytes(header)
	if err != nil {
		log.Error("failed to rlp encode header on write block", "error", err)
	}
	headerSize := uint32(len(rlpHeader))

	rlpBody, err := rlp.EncodeToBytes(body)
	if err != nil {
		log.Error("failed to rlp encode block on write block", "error", err)
	}
	data := append(rlpHeader, rlpBody...)

	// Store the RLP encoded block data with header size
	err = d.BlockDatabase.WriteBlock(block.NumberU64(), data, headerSize)
	if err != nil {
		log.Error("failed to write block to block db on write block", "error", err)
	}
}

func (d *db) ReadBody(height uint64) *types.Body {
	block := d.ReadBlock(height)
	if block == nil {
		return nil
	}
	return block.Body()

	// data, err := d.BlockDatabase.ReadBlock(height)
	// if err != nil || len(data) == 0 {
	// 	return nil
	// }
	// body := new(types.Body)
	// if err := rlp.DecodeBytes(data, body); err != nil {
	// 	return nil
	// }

	// return body
}

func (d *db) ReadHeader(height uint64) *types.Header {
	data, err := d.BlockDatabase.ReadHeader(height)
	if err != nil || len(data) == 0 {
		return nil
	}
	header := new(types.Header)
	if err := rlp.DecodeBytes(data, header); err != nil {
		return nil
	}
	return header
}

func (d *db) ReadBlock(height uint64) *types.Block {
	block, err := d.BlockDatabase.ReadBlock(height)
	if err != nil || len(block) == 0 {
		return nil
	}

	_, _, bodyData, err := rlp.Split(block)
	if err != nil {
		return nil
	}
	headerSize := len(block) - len(bodyData)
	headerData := block[:headerSize]

	body := new(types.Body)
	if err := rlp.DecodeBytes(bodyData, body); err != nil {
		return nil
	}
	header := new(types.Header)
	if err := rlp.DecodeBytes(headerData, header); err != nil {
		return nil
	}
	return types.NewBlockWithHeader(header).WithBody(*body).WithWithdrawals(body.Withdrawals)
}

func (d *db) HasBlock(height uint64) bool {
	ok, err := d.BlockDatabase.HasBlock(height)
	if err != nil {
		log.Error("failed to check if block exists in block db", "error", err)
		return false
	}
	return ok
}

func (d *db) ReadReceipts(hash common.Hash, number uint64, time uint64, config *params.ChainConfig) types.Receipts {
	receipts := rawdb.ReadRawReceipts(d.chainDb, hash, number)
	if receipts == nil {
		return nil
	}
	block := d.ReadBlock(number)
	if block == nil {
		return nil
	}
	header := block.Header()
	body := block.Body()
	var baseFee *big.Int
	if header == nil {
		baseFee = big.NewInt(0)
	} else {
		baseFee = header.BaseFee
	}

	// Compute effective blob gas price.
	var blobGasPrice *big.Int
	if header != nil && header.ExcessBlobGas != nil {
		blobGasPrice = eip4844.CalcBlobFee(*header.ExcessBlobGas)
	}
	if err := receipts.DeriveFields(config, hash, number, time, baseFee, blobGasPrice, body.Transactions); err != nil {
		log.Error("ReadReceipts: receipts.DeriveFields failed", "hash", hash, "number", number, "error", err)
		return nil
	}
	return receipts
}

// ReadTransaction retrieves a specific transaction from the database, along with
// its added positional metadata.
func (d *db) ReadTransaction(hash common.Hash) (*types.Transaction, common.Hash, uint64, uint64) {
	blockNumber := rawdb.ReadTxLookupEntry(d.chainDb, hash)
	if blockNumber == nil {
		return nil, common.Hash{}, 0, 0
	}
	block := d.ReadBlock(*blockNumber)
	if block == nil {
		return nil, common.Hash{}, 0, 0
	}
	for txIndex, tx := range block.Body().Transactions {
		if tx.Hash() == hash {
			return tx, block.Header().Hash(), *blockNumber, uint64(txIndex)
		}
	}
	return nil, common.Hash{}, 0, 0
}
