// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rawdb

import (
	"bytes"

	"github.com/ava-labs/coreth/ethdb"
)

// prefixIterator wraps an [ethdb.Iterator], returning keys that match a certain
// prefix and key length within an optionally provided [start, end] range (both inclusive).
// The prefix is removed from keys before returning them, and values remain untouched
type prefixIterator struct {
	ethdb.Iterator
	prefixLen  int
	keyLen     int
	start, end []byte
}

func NewPrefixIterator(db ethdb.Iteratee, prefix []byte, keyLen int, start, end []byte) ethdb.Iterator {
	return &prefixIterator{
		Iterator:  db.NewIterator(prefix, start),
		prefixLen: len(prefix),
		keyLen:    keyLen,
		start:     start,
		end:       end,
	}
}

// Next moves the iterator to the next key matching the prefix and expected key length.
// Returns true if there are more items to iterate.
func (it *prefixIterator) Next() bool {
	for it.Iterator.Next() {
		if len(it.Iterator.Key()) != it.prefixLen+it.keyLen {
			continue
		}
		if len(it.end) > 0 && bytes.Compare(it.Key(), it.end) > 0 {
			break
		}
		return true
	}
	return false
}

// Key returns the key after removing the prefix. The caller can expect the length of the
// returned byte slice to be [keyLen].
func (it *prefixIterator) Key() []byte {
	return it.Iterator.Key()[it.prefixLen:]
}

// valueWrapperIterator wraps an [ethdb.Iterator] and applies a given transformation function
// to values before returning them.
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

// Value returns the result of the transformation function being applied to values of the
// underlying iterator
func (it *valueWrapperIterator) Value() []byte {
	return it.val
}

// Next moves the iterator to the next value, and applies the tranformation function to the
// underlying iterator's value, so it can be returned from [it.Value()].
// If an error occurs in applying the transformation function, Next returns false and
// the error can be retrieved by calling [it.Error()].
// Returns true if there are more items to iterate.
func (it *valueWrapperIterator) Next() bool {
	if it.Iterator.Next() {
		it.val, it.err = it.f(it.Iterator.Value())
		return it.err == nil
	}
	return false
}

// Error returns an error if one occurred while calling the transformation function or
// if an error occurred in the underlying iterator.
func (it *valueWrapperIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}
