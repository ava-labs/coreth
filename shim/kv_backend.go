package shim

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/database"
	"github.com/ethereum/go-ethereum/common"
)

var (
	ErrRootMismatch             = errors.New("root mismatch")
	ErrStorageStateRootMismatch = errors.New("storage state root mismatch")
)

type (
	KV        = triedb.KV
	Batch     = triedb.Batch
	KVBackend = triedb.KVBackend
)

type KVTrieBackend struct {
	hashed  bool
	hash    common.Hash
	backend KVBackend
}

func (k *KVTrieBackend) Get(key []byte) ([]byte, error) {
	fmt.Printf("Get: %x\n", key)
	return k.backend.Get(key)
}

func (k *KVTrieBackend) Prefetch(key []byte) ([]byte, error) {
	fmt.Printf("Prefetch: %x\n", key)
	return k.backend.Get(key)
}

func (k *KVTrieBackend) Hash(batch Batch) common.Hash {
	if k.hashed {
		return k.hash
	}
	fmt.Printf("Update Total: %d\n", len(batch))
	for _, kv := range batch {
		fmt.Printf("Update: %x %x\n", kv.Key, kv.Value)
	}
	root, err := k.backend.Update(batch)
	if err != nil {
		panic(fmt.Sprintf("failed to update trie: %v", err))
	}
	k.hashed = true
	k.hash = root
	return root
}

func (k *KVTrieBackend) Commit(batch Batch, collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	if !k.hashed {
		k.Hash(batch)
	}
	return k.hash, nil, nil
}

// NewAccountTrieKV creates a new state trie backed by a key-value store.
// db is used for preimages
func NewAccountTrieKV(stateRoot common.Hash, kv KVBackend, db database.Database) (*StateTrie, error) {
	if stateRoot == types.EmptyRootHash {
		stateRoot = common.Hash{}
	}
	kvRoot := kv.Root()
	if kvRoot != stateRoot {
		return nil, fmt.Errorf("%w: expected %x, got %x", ErrRootMismatch, stateRoot, kvRoot)
	}

	tr := &Trie{
		backend: &KVTrieBackend{backend: kv},
		origin:  kvRoot,
	}
	return &StateTrie{trie: tr, db: db}, nil
}

// NewStorageTrieKV creates a new storage trie backed by a key-value store.
func NewStorageTrieKV(stateRoot common.Hash, account common.Hash, accountTrie *StateTrie) (*StateTrie, error) {
	if stateRoot == types.EmptyRootHash {
		stateRoot = common.Hash{}
	}
	if accountTrie.trie.origin != stateRoot {
		return nil, fmt.Errorf("%w: expected %x, got %x", ErrStorageStateRootMismatch, stateRoot, accountTrie.trie.origin)
	}
	tr := &Trie{
		parent:  accountTrie.trie,
		backend: accountTrie.trie.backend,
		prefix:  account.Bytes(),
	}
	return &StateTrie{trie: tr, db: accountTrie.db}, nil
}
