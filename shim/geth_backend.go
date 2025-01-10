package shim

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb/database"
	"github.com/ethereum/go-ethereum/common"
)

var _ Backend = (*LegacyBackend)(nil)

type LegacyBackend struct {
	hash   common.Hash
	hashed bool
	tr     *trie.Trie

	addrHash common.Hash
	writer   writer
}

type writer interface {
	MustUpdate(key, value []byte)
}

func NewLegacyBackend(
	stateRoot common.Hash, addrHash common.Hash, root common.Hash, db database.Database,
	writer writer,
) (*LegacyBackend, error) {
	trieID := trie.StateTrieID(root)
	if addrHash != (common.Hash{}) {
		trieID = trie.StorageTrieID(stateRoot, addrHash, root)
	}

	tr, err := trie.New(trieID, db)
	if err != nil {
		return nil, err
	}

	return &LegacyBackend{tr: tr, addrHash: addrHash, writer: writer}, nil
}

var target = common.Hex2Bytes("171ab08901be24769dbebedbdf7e0245486fbc64ab975cd431a39533032d5415")

func (b *LegacyBackend) Prefetch(key []byte) ([]byte, error) { return b.tr.Get(key) }
func (b *LegacyBackend) Get(key []byte) ([]byte, error) {
	val, err := b.tr.Get(key)
	if b.addrHash != (common.Hash{}) {
		fmt.Printf("Get: %x%x %x\n", b.addrHash, key, val)
		if bytes.Equal(key, target) {
			panic("found")
		}
	} else {
		fmt.Printf("Get: %x %x\n", key, val)
	}
	return val, err
}

func (b *LegacyBackend) Hash(batch Batch) common.Hash {
	if b.hashed {
		return b.hash
	}
	for _, kv := range batch {
		b.tr.MustUpdate(kv.Key, kv.Value)

		if b.writer == nil {
			continue
		}
		if b.addrHash != (common.Hash{}) {
			key := append(b.addrHash.Bytes(), kv.Key...)
			b.writer.MustUpdate(key, kv.Value)
		} else {
			b.writer.MustUpdate(kv.Key, kv.Value)
		}
	}
	b.hashed = true
	b.hash = b.tr.Hash()
	return b.hash
}

func (b *LegacyBackend) Commit(batch Batch, collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	if !b.hashed {
		b.Hash(batch)
	}
	return b.tr.Commit(collectLeaf)
}
