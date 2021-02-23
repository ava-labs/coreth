// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"

	//"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"

	"github.com/Determinant/cedrusdb-go"
)

var (
	errOpNotSupported = errors.New("this operation is not supported")
	errBatchDelete    = errors.New("error while Delete in a batch")
	errBatchPut       = errors.New("error while Put in a batch")
	errBatchWrite     = errors.New("error while writing a batch")
	errGet            = errors.New("error while Get")
	errPut            = errors.New("error while Put")
	errDBClosed       = errors.New("db is closed")
)

// Database implements ethdb.Database
type Database struct {
	cedrusdb.Cedrus
	isClosed bool
}

// HasAncient returns an error as we don't have a backing chain freezer.
func (db Database) HasAncient(kind string, number uint64) (bool, error) {
	return false, errOpNotSupported
}

// Ancient returns an error as we don't have a backing chain freezer.
func (db Database) Ancient(kind string, number uint64) ([]byte, error) { return nil, errOpNotSupported }

// Ancients returns an error as we don't have a backing chain freezer.
func (db Database) Ancients() (uint64, error) { return 0, errOpNotSupported }

// AncientSize returns an error as we don't have a backing chain freezer.
func (db Database) AncientSize(kind string) (uint64, error) { return 0, errOpNotSupported }

// AppendAncient returns an error as we don't have a backing chain freezer.
func (db Database) AppendAncient(number uint64, hash, header, body, receipts, td []byte) error {
	return errOpNotSupported
}

// TruncateAncients returns an error as we don't have a backing chain freezer.
func (db Database) TruncateAncients(items uint64) error { return errOpNotSupported }

// Sync returns an error as we don't have a backing chain freezer.
func (db Database) Sync() error { return errOpNotSupported }

// NewBatch implements ethdb.Database
func (db Database) NewBatch() ethdb.Batch {
	return Batch{&batchInner{
		writes: nil,
		size:   0,
		db:     &db,
	}}
}

// NewIterator implements ethdb.Database
func (db Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	panic(errOpNotSupported)
}

// NewIteratorWithStart implements ethdb.Database
func (db Database) NewIteratorWithStart(start []byte) ethdb.Iterator {
	panic(errOpNotSupported)
}

type batchOp struct {
	key      []byte
	val      []byte
	isDelete bool
}

type batchInner struct {
	writes []batchOp
	size   int
	db     *Database
}

// Batch implements ethdb.Batch
type Batch struct{ *batchInner }

// Replay implements ethdb.Batch
func (batch Batch) Replay(writer ethdb.KeyValueWriter) error {
	for _, w := range batch.writes {
		if w.isDelete {
			if err := writer.Delete(w.key); err != nil {
				return err
			}
		} else {
			if err := writer.Put(w.key, w.val); err != nil {
				return err
			}
		}
	}
	return nil
}

// Delete implements ethdb.Batch
func (batch Batch) Delete(key []byte) error {
	batch.size++
	batch.writes = append(batch.writes, batchOp{
		key:      key,
		val:      nil,
		isDelete: true,
	})
	return nil
}

// Put implements ethdb.Batch
func (batch Batch) Put(key []byte, val []byte) error {
	batch.size += len(val)
	batch.writes = append(batch.writes, batchOp{
		key:      key,
		val:      val,
		isDelete: false,
	})
	return nil
}

// Reset implements ethdb.Batch
func (batch Batch) Reset() {
	batch.size = 0
	batch.writes = nil
}

// ValueSize implements ethdb.Batch
func (batch Batch) ValueSize() int {
	return batch.size
}

func (batch Batch) Write() error {
	if batch.db.isClosed {
		return errDBClosed
	}
	b := batch.db.Cedrus.NewWriteBatch()
	for _, w := range batch.writes {
		if w.isDelete {
			if b.Delete(w.key) != 0 {
				return errBatchDelete
			}
		} else {
			if b.Put(w.key, w.val) != 0 {
				return errBatchPut
			}
		}
	}
	if b.Write() != 0 {
		return errBatchWrite
	}
	return nil
}

// Compact implements ethdb.Database
func (db Database) Compact(start []byte, limit []byte) error {
	return nil
}

// Stat implements ethdb.Database
func (db Database) Stat(property string) (string, error) {
	return "n/a", nil
}

// Put implements ethdb.Database
func (db Database) Put(key []byte, value []byte) error {
	if db.isClosed {
		return errDBClosed
	}
	if db.Cedrus.Put(key, value) == 0 {
		return nil
	}
	return errPut
}

// Delete implements ethdb.Database
func (db Database) Delete(key []byte) error {
	if db.isClosed {
		return errDBClosed
	}
	// FIXME: check the kind of error from Delete
	db.Cedrus.Delete(key)
	return nil
}

// Has implements ethdb.Database
func (db Database) Has(key []byte) (res bool, err error) {
	if db.isClosed {
		return false, errDBClosed
	}
	ret, vr := db.Cedrus.Get(key)
	err = nil
	// FIXME: check the kind of error from Get
	if ret == 0 {
		vr.Free()
		res = true
	} else {
		res = false
	}
	return
}

// Get implements ethdb.Database
func (db Database) Get(key []byte) (res []byte, err error) {
	if db.isClosed {
		return nil, errDBClosed
	}
	ret, vr := db.Cedrus.Get(key)
	// FIXME: check the kind of error from Get
	if ret != 0 {
		res = nil
		err = errGet
		return
	}

	res = make([]byte, len(vr.AsBytes()))
	err = nil
	copy(res, vr.AsBytes())
	vr.Free()
	return
}

// NewDatabase creates a database wrapper with the given CedrusDB handle.
func NewDatabase(cdb cedrusdb.Cedrus) Database {
	return Database{
		Cedrus:   cdb,
		isClosed: false,
	}
}

// Close implements ethdb.Database
func (db Database) Close() error {
	log.Info("flush and close CedrusDB")
	db.Free()
	db.isClosed = true
	return nil
}
