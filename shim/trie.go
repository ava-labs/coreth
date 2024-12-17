package shim

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb/database"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

type Trie struct {
	changes Batch
	backend Backend

	prefix []byte
	parent *Trie
	origin common.Hash
}

type Backend interface {
	Get(key []byte) ([]byte, error)
	Hash(batch Batch) common.Hash
	Commit(batch Batch, collectLeaf bool) (common.Hash, *trienode.NodeSet, error)
}

func NewStateTrie(backend Backend, db database.Database) *StateTrie {
	return &StateTrie{
		trie: Trie{backend: backend},
		db:   db,
	}
}

func (t *Trie) getKey(key []byte) []byte {
	return append(t.prefix, key...)
}

// Update batches updates to the trie
func (t *Trie) Update(key, value []byte) error {
	key = t.getKey(key)
	value = bytes.Clone(value)
	if t.parent != nil {
		t.parent.changes = append(t.parent.changes, KV{Key: key, Value: value})
	} else {
		t.changes = append(t.changes, KV{Key: key, Value: value})
	}
	return nil
}

func (t *Trie) Delete(key []byte) error {
	key = t.getKey(key)
	if t.parent != nil {
		t.parent.changes = append(t.parent.changes, KV{Key: key})
	} else {
		t.changes = append(t.changes, KV{Key: key})
	}
	return nil
}

func (t *Trie) Hash() common.Hash {
	if t.parent != nil {
		return common.Hash{}
	}

	return t.backend.Hash(t.changes)
}

func (t *Trie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	if t.parent != nil {
		return common.Hash{}, nil, nil
	}

	return t.backend.Commit(t.changes, collectLeaf)
}

func (t *Trie) Get(key []byte) ([]byte, error) {
	key = t.getKey(key)
	return t.backend.Get(key)
}

func (t *Trie) Copy() *Trie {
	fmt.Printf("Copy requested (%d changes): %p: %x\n", len(t.changes), t, t.prefix)
	return t
}

func (t *Trie) NodeIterator(start []byte) (trie.NodeIterator, error) { panic("not implemented") }
func (t *Trie) MustNodeIterator(start []byte) trie.NodeIterator      { panic("not implemented") }
func (t *Trie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error { panic("not implemented") }
