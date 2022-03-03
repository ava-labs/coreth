// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/coreth/ethdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/olekukonko/tablewriter"
)

var (
	getDB stat
	putDB stat
)

type counter uint64

func (c counter) String() string {
	return fmt.Sprintf("%d", c)
}

func (c counter) Percentage(current uint64) string {
	return fmt.Sprintf("%d", current*100/uint64(c))
}

// stat stores sizes and count for a parameter
type stat struct {
	size  common.StorageSize
	count counter
	l     sync.RWMutex
}

// Add size to the stat and increase the counter by 1
func (s *stat) Add(size common.StorageSize) {
	s.l.Lock()
	defer s.l.Unlock()
	s.size += size
	s.count++
}

func (s *stat) AddBytes(b []byte) {
	s.Add(common.StorageSize(len(b)))
}

func (s *stat) Size() string {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.size.String()
}

func (s *stat) Count() string {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.count.String()
}

func DBUsageLogger(s chan struct{}, f *os.File) {
	t := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-s:
			return
		case <-t.C:
			// Display the database statistic.
			stats := [][]string{
				{"GET", getDB.Size(), getDB.Count()},
				{"PUT", putDB.Size(), putDB.Count()},
			}
			table := tablewriter.NewWriter(f)
			table.SetHeader([]string{"Op", "Size", "Items"})
			table.AppendBulk(stats)
			table.Render()
		}
	}
}

// Database implements ethdb.Database
type Database struct{ database.Database }

func (db Database) Get(key []byte) ([]byte, error) {
	log.Info("get")
	dat, err := db.Database.Get(key)
	getDB.AddBytes(dat)
	return dat, err
}

func (db Database) Put(key []byte, value []byte) error {
	log.Info("put")
	putDB.AddBytes(value)
	return db.Database.Put(key, value)
}

// NewBatch implements ethdb.Database
func (db Database) NewBatch() ethdb.Batch { return Batch{db.Database.NewBatch()} }

// NewIterator implements ethdb.Database
//
// Note: This method assumes that the prefix is NOT part of the start, so there's
// no need for the caller to prepend the prefix to the start.
func (db Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// avalanchego's database implementation assumes that the prefix is part of the
	// start, so it is added here (if it is provided).
	if len(prefix) > 0 {
		newStart := make([]byte, len(prefix)+len(start))
		copy(newStart, prefix)
		copy(newStart[len(prefix):], start)
		start = newStart
	}
	return db.Database.NewIteratorWithStartAndPrefix(start, prefix)
}

// NewIteratorWithStart implements ethdb.Database
func (db Database) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStart(start)
}

// Batch implements ethdb.Batch
type Batch struct{ database.Batch }

// ValueSize implements ethdb.Batch
func (batch Batch) ValueSize() int { return batch.Batch.Size() }

// Replay implements ethdb.Batch
func (batch Batch) Replay(w ethdb.KeyValueWriter) error { return batch.Batch.Replay(w) }
