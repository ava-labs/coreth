// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"

	//"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/Determinant/cedrusdb-go"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

var (
	//errOpNotSupported = errors.New("this operation is not supported")
	errBatchDelete = errors.New("error while Delete in a batch")
	errBatchPut    = errors.New("error while Put in a batch")
	errBatchWrite  = errors.New("error while writing a batch")
	errGet         = errors.New("error while Get")
	errPut         = errors.New("error while Put")
	errDBClosed    = errors.New("db is closed")
)

type batchOp struct {
	key      []byte
	val      []byte
	isDelete bool
}

type batchInner struct {
	writes []batchOp
	size   int
	db     *CedrusDatabase
}

// CedrusBatch implements database.Batch
type CedrusBatch struct{ *batchInner }

// NewBatch implements database.Database
func (db CedrusDatabase) NewBatch() database.Batch {
	return CedrusBatch{&batchInner{
		writes: nil,
		size:   0,
		db:     &db,
	}}
}

// Inner implements database.Database
func (batch CedrusBatch) Inner() database.Batch {
	// FIXME: need to implement this properly
	return batch
}

// Replay implements ethdb.Batch
func (batch CedrusBatch) Replay(writer database.KeyValueWriter) error {
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

// Delete implements database.Batch
func (batch CedrusBatch) Delete(key []byte) error {
	batch.size++
	batch.writes = append(batch.writes, batchOp{
		key:      key,
		val:      nil,
		isDelete: true,
	})
	return nil
}

// Put implements database.Batch
func (batch CedrusBatch) Put(key []byte, val []byte) error {
	batch.size += len(val)
	batch.writes = append(batch.writes, batchOp{
		key:      key,
		val:      val,
		isDelete: false,
	})
	return nil
}

// Reset implements database.Batch
func (batch CedrusBatch) Reset() {
	batch.size = 0
	batch.writes = nil
}

// ValueSize implements database.Batch
func (batch CedrusBatch) ValueSize() int {
	return batch.size
}

func (batch CedrusBatch) Write() error {
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
			if b.Put(w.key, w.val) < 0 {
				return errBatchPut
			}
		}
	}
	if b.Write() != 0 {
		return errBatchWrite
	}
	return nil
}

// CedrusDatabase implements database.Database
type CedrusDatabase struct {
	cedrusdb.Cedrus
	isClosed bool
}

// NewCedrusDatabase creates a database wrapper with the given CedrusDB handle.
func NewCedrusDatabase(cdb cedrusdb.Cedrus) CedrusDatabase {
	return CedrusDatabase{
		Cedrus:   cdb,
		isClosed: false,
	}
}

// Close implements database.Database
func (db CedrusDatabase) Close() error {
	log.Info("flush and close CedrusDB")
	db.Free()
	db.isClosed = true
	return nil
}

// Compact implements database.Database
func (db CedrusDatabase) Compact(start []byte, limit []byte) error {
	return nil
}

// Stat implements database.Database
func (db CedrusDatabase) Stat(property string) (string, error) {
	return "n/a", nil
}

// Put implements database.Database
func (db CedrusDatabase) Put(key []byte, value []byte) error {
	if db.isClosed {
		return errDBClosed
	}
	if db.Cedrus.Put(key, value) >= 0 {
		return nil
	}
	return errPut
}

// Delete implements database.Database
func (db CedrusDatabase) Delete(key []byte) error {
	if db.isClosed {
		return errDBClosed
	}
	// FIXME: check the kind of error from Delete
	db.Cedrus.Delete(key)
	return nil
}

// Has implements database.Database
func (db CedrusDatabase) Has(key []byte) (res bool, err error) {
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

// Get implements database.Database
func (db CedrusDatabase) Get(key []byte) (res []byte, err error) {
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

// NewIterator implements database.Database
func (db CedrusDatabase) NewIterator() database.Iterator {
	panic(errOpNotSupported)
}

// NewIteratorWithPrefix implements database.Database
func (db CedrusDatabase) NewIteratorWithPrefix([]byte) database.Iterator {
	panic(errOpNotSupported)
}

// NewIteratorWithStartAndPrefix implements database.Database
func (db CedrusDatabase) NewIteratorWithStartAndPrefix(start []byte, prefix []byte) database.Iterator {
	panic(errOpNotSupported)
}

// NewIteratorWithStart implements database.Database
func (db CedrusDatabase) NewIteratorWithStart(start []byte) database.Iterator {
	panic(errOpNotSupported)
}

// below is for geth integration

// CedrusEthDatabase implements database.Database
type CedrusEthDatabase struct {
	database.Database
}

// HasAncient returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) HasAncient(kind string, number uint64) (bool, error) {
	return false, errOpNotSupported
}

// Ancient returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) Ancient(kind string, number uint64) ([]byte, error) {
	return nil, errOpNotSupported
}

// Ancients returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) Ancients() (uint64, error) { return 0, errOpNotSupported }

// AncientSize returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) AncientSize(kind string) (uint64, error) { return 0, errOpNotSupported }

// AppendAncient returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) AppendAncient(number uint64, hash, header, body, receipts, td []byte) error {
	return errOpNotSupported
}

// TruncateAncients returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) TruncateAncients(items uint64) error { return errOpNotSupported }

// Sync returns an error as we don't have a backing chain freezer.
func (db CedrusEthDatabase) Sync() error { return errOpNotSupported }

// NewBatch implements ethdb.Database
func (db CedrusEthDatabase) NewBatch() ethdb.Batch {
	return CedrusEthBatch{db.Database.NewBatch()}
}

// NewIterator implements ethdb.Database
func (db CedrusEthDatabase) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStartAndPrefix(start, prefix)
}

// NewIteratorWithStart implements ethdb.Database
func (db CedrusEthDatabase) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStart(start)
}

// CedrusEthBatch implements ethdb.Batch
type CedrusEthBatch struct{ database.Batch }

// Replay implements ethdb.Batch
func (batch CedrusEthBatch) Replay(writer ethdb.KeyValueWriter) error {
	return batch.Batch.Replay(writer)
}
