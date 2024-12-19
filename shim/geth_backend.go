package shim

import (
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
}

func NewLegacyBackend(
	stateRoot common.Hash, addrHash common.Hash, root common.Hash, db database.Database,
) (*LegacyBackend, error) {
	trieID := trie.StateTrieID(root)
	if addrHash != (common.Hash{}) {
		trieID = trie.StorageTrieID(stateRoot, addrHash, root)
	}

	tr, err := trie.New(trieID, db)
	if err != nil {
		return nil, err
	}

	return &LegacyBackend{tr: tr}, nil
}

func (b *LegacyBackend) Get(key []byte) ([]byte, error) { return b.tr.Get(key) }

func (b *LegacyBackend) Hash(batch Batch) common.Hash {
	if b.hashed {
		return b.hash
	}
	for _, kv := range batch {
		b.tr.MustUpdate(kv.Key, kv.Value)
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
