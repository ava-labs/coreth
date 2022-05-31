// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rawdb

import (
	"bytes"

	"github.com/ava-labs/coreth/ethdb"
	"github.com/ethereum/go-ethereum/common"
)

type prefixIterator struct {
	ethdb.Iterator
	prefixLen  int
	start, end []byte
}

func NewPrefixIterator(db ethdb.Iteratee, prefix []byte, start, end []byte) ethdb.Iterator {
	return &prefixIterator{
		Iterator:  db.NewIterator(prefix, start),
		prefixLen: len(prefix),
		start:     start,
		end:       end,
	}
}

func (it *prefixIterator) Next() bool {
	for it.Iterator.Next() {
		if len(it.Iterator.Key()) != it.prefixLen+common.HashLength {
			continue
		}
		if it.end != nil && bytes.Compare(it.end, it.Key()) < 0 {
			break
		}
		return true
	}
	return false
}

func (it *prefixIterator) Key() []byte {
	return it.Iterator.Key()[it.prefixLen:]
}

type valueWrapperIterator struct {
	ethdb.Iterator
	f   func([]byte) ([]byte, error)
	err error
	val []byte
}

func NewValueWrapperIterator(it ethdb.Iterator, f func([]byte) ([]byte, error)) *valueWrapperIterator {
	return &valueWrapperIterator{
		Iterator: it,
		f:        f,
	}
}

func (it *valueWrapperIterator) Value() []byte {
	return it.val
}

func (it *valueWrapperIterator) Next() bool {
	if it.Iterator.Next() {
		it.val, it.err = it.f(it.Iterator.Value())
		return it.err == nil
	}
	return false
}

func (it *valueWrapperIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}
