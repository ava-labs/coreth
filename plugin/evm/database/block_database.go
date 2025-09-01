package database

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
)

var (
	_ ethdb.Database = (*wrappedBlockDatabase)(nil)

	blockdbMigrationPrefix = []byte("blockdb_migration")

	// chaindb database scheme prefixes
	// should these be exported from libevm or just keep a copy of the prefix here?
	// these should really never change, so it might be fine to keep a copy here to avoid
	// libevm changes
	chaindbHeaderPrefix    = []byte("h")
	chaindbBlockBodyPrefix = []byte("b")
)

type wrappedBlockDatabase struct {
	blockdb database.BlockDatabase
	ethdb.Database

	bodyCache   map[common.Hash][]byte
	headerCache map[common.Hash][]byte
	migrator    *blockDatabaseMigrator
}

type wrappedBlockDatabaseBatch struct {
	database wrappedBlockDatabase
	ethdb.Batch
}

func NewWrappedBlockDatabase(db database.Database, blockdb database.BlockDatabase, chaindb ethdb.Database, withMigrator bool) (*wrappedBlockDatabase, error) {
	var migrator *blockDatabaseMigrator
	if withMigrator {
		migratordb := prefixdb.New(blockdbMigrationPrefix, db)
		var err error
		migrator, err = NewBlockDatabaseMigrator(migratordb, blockdb, chaindb)
		if err != nil {
			return nil, err
		}
		if migrator.status != migrationStatusCompleted {
			if err := migrator.Migrate(); err != nil {
				return nil, err
			}
		} else {
			log.Info("blockdb migration completed")
		}
	}
	return &wrappedBlockDatabase{
		blockdb:     blockdb,
		Database:    chaindb,
		bodyCache:   make(map[common.Hash][]byte),
		headerCache: make(map[common.Hash][]byte),
		migrator:    migrator,
	}, nil
}

func (db wrappedBlockDatabase) Put(key []byte, value []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Put(key, value)
	}

	// save block header or body to cache. Only write to database if we have both
	blockNumber, blockHash := blockNumberAndHashFromKey(key)
	if isBodyKey(key) {
		db.bodyCache[blockHash] = value
	} else if isHeaderKey(key) {
		db.headerCache[blockHash] = value
	}
	if _, ok := db.bodyCache[blockHash]; !ok {
		return nil
	} else if _, ok := db.headerCache[blockHash]; !ok {
		return nil
	}

	// encode block data as RLP array before writing to blockdb
	encodedBlock, err := encodeBlockData(db.headerCache[blockHash], db.bodyCache[blockHash])
	if err != nil {
		return err
	}
	err = db.blockdb.WriteBlock(blockNumber, encodedBlock)
	if err != nil {
		return err
	}
	return nil
}

func (db wrappedBlockDatabase) Delete(key []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Delete(key)
	}

	// note: we should not need to delete block data from the ethdb during migration
	// because all new blocks will be written to blockdb.
	// For old blocks, we should not be deleting them from the ethdb during migration anyways.
	// If the block is canonical it should not be deleted. If not canonical, it
	// will be deleted by the migrator.

	// no-op since blockdb does not support deleting written blocks
	height, hash := blockNumberAndHashFromKey(key)
	log.Info("blockdb does not support deleting written blocks", "height", height, "hash", hash)
	return nil
}

func (db wrappedBlockDatabase) Has(key []byte) (bool, error) {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Has(key)
	}
	blockNumber, _ := blockNumberAndHashFromKey(key)
	hasBlock, err := db.blockdb.HasBlock(blockNumber)
	if err != nil {
		return false, err
	}
	if !hasBlock && db.migrator != nil && db.migrator.status != migrationStatusCompleted {
		return db.Database.Has(key)
	}
	return hasBlock, nil
}

func (db wrappedBlockDatabase) Get(key []byte) ([]byte, error) {
	if !isBlockBodyOrHeaderKey(key) {
		return db.Database.Get(key)
	}
	blockNumber, _ := blockNumberAndHashFromKey(key)

	// read block from blockdb
	encodedBlock, err := db.blockdb.ReadBlock(blockNumber)
	if err != nil {
		return nil, err
	}
	header, body, err := decodeBlockData(encodedBlock)
	if err != nil {
		return nil, err
	}

	var data []byte
	if isBodyKey(key) {
		data = body
	} else if isHeaderKey(key) {
		data = header
	}

	// fallback to ethdb if block is not found and we are still migrating
	// this means worse performance during migration if getting blocks that are not migrated.
	// However, since new blocks will always be in blockdb, this only happens when an old block is requested, which should be rare.
	if data == nil && db.migrator != nil && db.migrator.status != migrationStatusCompleted {
		return db.Database.Get(key)
	}

	return data, nil
}

func (db wrappedBlockDatabase) NewBatch() ethdb.Batch {
	return wrappedBlockDatabaseBatch{
		database: db,
		Batch:    db.Database.NewBatch(),
	}
}

func (db wrappedBlockDatabase) Close() error {
	if db.migrator != nil {
		db.migrator.Stop()
	}
	err := db.blockdb.Close()
	if err != nil {
		return err
	}
	return db.Database.Close()
}

func (batch wrappedBlockDatabaseBatch) Put(key []byte, value []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return batch.Batch.Put(key, value)
	}
	return batch.database.Put(key, value)
}

func (batch wrappedBlockDatabaseBatch) Delete(key []byte) error {
	if !isBlockBodyOrHeaderKey(key) {
		return batch.Batch.Delete(key)
	}
	return batch.database.Delete(key)
}

// isBlockBodyOrHeaderKey checks if the key is the key rawdb uses to
// store rlp encoded block header and body.
func isBlockBodyOrHeaderKey(key []byte) bool {
	return isBodyKey(key) || isHeaderKey(key)
}

func blockNumberAndHashFromKey(key []byte) (uint64, common.Hash) {
	var prefixLen int
	if isBodyKey(key) {
		prefixLen = len(chaindbBlockBodyPrefix)
	} else if isHeaderKey(key) {
		prefixLen = len(chaindbHeaderPrefix)
	}
	blockNumber := binary.BigEndian.Uint64(key[prefixLen : prefixLen+8])
	blockHash := common.BytesToHash(key[prefixLen+8:])
	return blockNumber, blockHash
}

// decodeBlockData decodes the RLP-encoded block data back into header and body
func decodeBlockData(encodedBlock []byte) (header []byte, body []byte, err error) {
	var blockData [][]byte
	if err := rlp.DecodeBytes(encodedBlock, &blockData); err != nil {
		return nil, nil, err
	}

	if len(blockData) != 2 {
		return nil, nil, fmt.Errorf("invalid block data format: expected 2 elements, got %d", len(blockData))
	}

	return blockData[0], blockData[1], nil
}

func encodeBlockData(header []byte, body []byte) ([]byte, error) {
	return rlp.EncodeToBytes([][]byte{header, body})
}

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func isBodyKey(key []byte) bool {
	if len(key) != len(chaindbBlockBodyPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chaindbBlockBodyPrefix)
}

func isHeaderKey(key []byte) bool {
	if len(key) != len(chaindbHeaderPrefix)+8+32 {
		return false
	}
	return bytes.HasPrefix(key, chaindbHeaderPrefix)
}
