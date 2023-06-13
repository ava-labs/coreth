// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/trie"
)

type accessRecorder interface {
	recordRead(key, val []byte) error
	recordNotFound(key []byte) error
}

type firstAccessRecorder struct {
	db     ethdb.KeyValueStore
	prefix []byte
}

func (r *firstAccessRecorder) recordRead(key, val []byte) error {
	key = append(r.prefix, key...)
	if ok, err := r.db.Has(key); err != nil {
		return err
	} else if ok {
		return nil
	}

	return r.db.Put(key, append([]byte{rawdb.OpRead}, val...))
}

func (r *firstAccessRecorder) recordNotFound(key []byte) error {
	key = append(r.prefix, key...)
	if ok, err := r.db.Has(key); err != nil {
		return err
	} else if ok {
		return nil
	}

	return r.db.Put(key, []byte{rawdb.OpNotFound})
}

type accessRecordingTrie struct {
	Trie
	accessRecorder
}

func newAccessRecordingTrie(tr Trie, accessRecorder accessRecorder) *accessRecordingTrie {
	return &accessRecordingTrie{
		Trie:           tr,
		accessRecorder: accessRecorder,
	}
}

func (t *accessRecordingTrie) TryGet(key []byte) ([]byte, error) {
	val, err := t.Trie.TryGet(key)
	if err != nil {
		if _, ok := err.(*trie.MissingNodeError); ok {
			if err := t.recordNotFound(key); err != nil {
				return nil, err
			}
		}
		return val, err
	}

	if err := t.recordRead(key, val); err != nil {
		return nil, err
	}
	return val, nil
}
