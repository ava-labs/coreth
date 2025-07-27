// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethblockdb

import (
	"math/big"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/coreth/consensus/misc/eip4844"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/prque"
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
	UnindexTransactions(from uint64, to uint64, interrupt chan struct{}, hook func(uint64) bool, report bool)
	Close() error
}

type db struct {
	database.BlockDatabase
	chainDb ethdb.Database
}

func New(database database.BlockDatabase, chainDb ethdb.Database) *db {
	return &db{BlockDatabase: database, chainDb: chainDb}
}

func Copy(d Database, chainDb ethdb.Database) Database {
	db, ok := d.(*db)
	if !ok {
		return nil
	}
	return New(db.BlockDatabase, chainDb)
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

	rawdb.WriteHeaderNumber(d.chainDb, block.Hash(), block.NumberU64())
}

func (d *db) ReadBody(height uint64) *types.Body {
	data, err := d.BlockDatabase.ReadBody(height)
	if err != nil || len(data) == 0 {
		return nil
	}
	body := new(types.Body)
	if err := rlp.DecodeBytes(data, body); err != nil {
		return nil
	}
	return body
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
	if block == nil || block.Hash() != hash {
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

func (d *db) Close() error {
	return d.BlockDatabase.Close()
}

type blockTxHashes struct {
	number uint64
	hashes []common.Hash
}

// UnindexTransactions removes txlookup indices of the specified block range.
// The from is included while to is excluded.
//
// There is a passed channel, the whole procedure will be interrupted if any
// signal received.
func (d *db) UnindexTransactions(from uint64, to uint64, interrupt chan struct{}, hook func(uint64) bool, report bool) {
	// short circuit for invalid range
	if from >= to {
		return
	}
	var (
		hashesCh = d.iterateTransactions(from, to, false, interrupt)
		batch    = d.chainDb.NewBatch()
		start    = time.Now()
		logged   = start.Add(-7 * time.Second)

		// we expect the first number to come in to be [from]. Therefore, setting
		// nextNum to from means that the queue gap-evaluation will work correctly
		nextNum     = from
		queue       = prque.New[int64, *blockTxHashes](nil)
		blocks, txs = 0, 0 // for stats reporting
	)
	// Otherwise spin up the concurrent iterator and unindexer
	for delivery := range hashesCh {
		// Push the delivery into the queue and process contiguous ranges.
		queue.Push(delivery, -int64(delivery.number))
		for !queue.Empty() {
			// If the next available item is gapped, return
			if _, priority := queue.Peek(); -priority != int64(nextNum) {
				break
			}
			// For testing
			if hook != nil && !hook(nextNum) {
				break
			}
			delivery := queue.PopItem()
			nextNum = delivery.number + 1
			rawdb.DeleteTxLookupEntries(batch, delivery.hashes)
			txs += len(delivery.hashes)
			blocks++

			// If enough data was accumulated in memory or we're at the last block, dump to disk
			// A batch counts the size of deletion as '1', so we need to flush more
			// often than that.
			if blocks%1000 == 0 {
				rawdb.WriteTxIndexTail(batch, nextNum)
				if err := batch.Write(); err != nil {
					log.Crit("Failed writing batch to db", "error", err)
					return
				}
				batch.Reset()
			}
			// If we've spent too much time already, notify the user of what we're doing
			if time.Since(logged) > 8*time.Second {
				log.Info("Unindexing transactions", "blocks", blocks, "txs", txs, "total", to-from, "elapsed", common.PrettyDuration(time.Since(start)))
				logged = time.Now()
			}
		}
	}
	// Flush the new indexing tail and the last committed data. It can also happen
	// that the last batch is empty because nothing to unindex, but the tail has to
	// be flushed anyway.
	rawdb.WriteTxIndexTail(batch, nextNum)
	if err := batch.Write(); err != nil {
		log.Crit("Failed writing batch to db", "error", err)
		return
	}
	logger := log.Debug
	if report {
		logger = log.Info
	}
	select {
	case <-interrupt:
		logger("Transaction unindexing interrupted", "blocks", blocks, "txs", txs, "tail", to, "elapsed", common.PrettyDuration(time.Since(start)))
	default:
		logger("Unindexed transactions", "blocks", blocks, "txs", txs, "tail", to, "elapsed", common.PrettyDuration(time.Since(start)))
	}
}

// iterateTransactions iterates over all transactions in the (canon) block
// number(s) given, and yields the hashes on a channel. If there is a signal
// received from interrupt channel, the iteration will be aborted and result
// channel will be closed.
func (d *db) iterateTransactions(from uint64, to uint64, reverse bool, interrupt chan struct{}) chan *blockTxHashes {
	// One thread sequentially reads data from db
	type numberRlp struct {
		number uint64
		rlp    rlp.RawValue
	}
	if to == from {
		return nil
	}
	threads := to - from
	if cpus := runtime.NumCPU(); threads > uint64(cpus) {
		threads = uint64(cpus)
	}
	var (
		rlpCh    = make(chan *numberRlp, threads*2)     // we send raw rlp over this channel
		hashesCh = make(chan *blockTxHashes, threads*2) // send hashes over hashesCh
	)
	// lookup runs in one instance
	lookup := func() {
		n, end := from, to
		if reverse {
			n, end = to-1, from-1
		}
		defer close(rlpCh)
		for n != end {
			data, err := d.BlockDatabase.ReadBody(n)
			if err != nil {
				log.Warn("Failed to read block body", "block", n, "error", err)
				return
			}
			// Feed the block to the aggregator, or abort on interrupt
			select {
			case rlpCh <- &numberRlp{n, data}:
			case <-interrupt:
				return
			}
			if reverse {
				n--
			} else {
				n++
			}
		}
	}
	// process runs in parallel
	var nThreadsAlive atomic.Int32
	nThreadsAlive.Store(int32(threads))
	process := func() {
		defer func() {
			// Last processor closes the result channel
			if nThreadsAlive.Add(-1) == 0 {
				close(hashesCh)
			}
		}()
		for data := range rlpCh {
			var body types.Body
			if err := rlp.DecodeBytes(data.rlp, &body); err != nil {
				log.Warn("Failed to decode block body", "block", data.number, "error", err)
				return
			}
			var hashes []common.Hash
			for _, tx := range body.Transactions {
				hashes = append(hashes, tx.Hash())
			}
			result := &blockTxHashes{
				hashes: hashes,
				number: data.number,
			}
			// Feed the block to the aggregator, or abort on interrupt
			select {
			case hashesCh <- result:
			case <-interrupt:
				return
			}
		}
	}
	go lookup() // start the sequential db accessor
	for i := 0; i < int(threads); i++ {
		go process()
	}
	return hashesCh
}
