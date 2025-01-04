package legacy

import (
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

var _ triedb.KVBackend = &Legacy{}

type Legacy struct {
	triedb      *triedb.Database
	root        common.Hash
	count       uint64
	dereference bool
}

func New(triedb *triedb.Database, root common.Hash, count uint64, dereference bool) *Legacy {
	return &Legacy{
		triedb:      triedb,
		root:        root,
		count:       count,
		dereference: dereference,
	}
}

func getAccountRoot(tr *trie.Trie, accHash common.Hash) (common.Hash, error) {
	root := types.EmptyRootHash
	accBytes, err := tr.Get(accHash[:])
	if err != nil {
		return common.Hash{}, err
	}
	if len(accBytes) > 0 {
		var acc types.StateAccount
		if err := rlp.DecodeBytes(accBytes, &acc); err != nil {
			return common.Hash{}, fmt.Errorf("failed to decode account: %w", err)
		}
		root = acc.Root
	}
	return root, nil
}

func (l *Legacy) Update(batch triedb.Batch) (common.Hash, error) {
	accounts, err := trie.New(trie.StateTrieID(l.root), l.triedb)
	if err != nil {
		return common.Hash{}, err
	}
	// Process the storage tries first, this means we can access the root for the
	// storage tries before they are updated in the account trie. Necessary for
	// the hash scheme.
	tries := make(map[common.Hash]*trie.Trie)
	for _, kv := range batch {
		accHash := common.BytesToHash(kv.Key[:32])
		if len(kv.Key) == 32 {
			if len(kv.Value) == 0 {
				// this trie is DELETED, so if it was updated before these updates should not be applied
				// further updates shold apply to an empty trie
				tries[accHash], err = trie.New(trie.StorageTrieID(l.root, accHash, types.EmptyRootHash), l.triedb)
				if err != nil {
					return common.Hash{}, fmt.Errorf("failed to create storage trie %x: %w", accHash, err)
				}
			}

			// otherwise, skip account updates for now
			continue
		}

		tr, ok := tries[accHash]
		if !ok {
			root, err := getAccountRoot(accounts, accHash)
			if err != nil {
				return common.Hash{}, err
			}
			tr, err = trie.New(trie.StorageTrieID(l.root, accHash, root), l.triedb)
			if err != nil {
				return common.Hash{}, fmt.Errorf("failed to create storage trie %x: %w", accHash, err)
			}
			tries[accHash] = tr
		}

		// Update the storage trie
		tr.MustUpdate(kv.Key[32:], kv.Value)
	}

	// Hash the storage tries
	nodes := trienode.NewMergedNodeSet()
	for _, tr := range tries {
		_, set, err := tr.Commit(false)
		if err != nil {
			return common.Hash{}, err
		}
		if set != nil {
			nodes.Merge(set)
		}
	}

	// Update the account trie
	for _, kv := range batch {
		if len(kv.Key) == 64 {
			continue
		}
		accounts.MustUpdate(kv.Key, kv.Value)
	}

	// Verify account trie updates match the storage trie updates
	for accHash, tr := range tries {
		root, err := getAccountRoot(accounts, accHash)
		if err != nil {
			return common.Hash{}, err
		}
		if root != tr.Hash() {
			return common.Hash{}, fmt.Errorf("account %x trie root mismatch (%x != %x)", accHash, root, tr.Hash())
		}
	}

	next, set, err := accounts.Commit(true)
	if err != nil {
		return common.Hash{}, err
	}
	if set != nil {
		nodes.Merge(set)
	}

	if err := l.triedb.Update(next, l.root, l.count, nodes, nil); err != nil {
		return common.Hash{}, err
	}

	// TODO: fix hashdb scheme later
	l.root = next
	l.count++
	return next, nil
}

func (l *Legacy) Commit(root common.Hash) error       { return l.triedb.Commit(root, false) }
func (l *Legacy) Close() error                        { return nil }
func (l *Legacy) Get(key []byte) ([]byte, error)      { panic("implement me") }
func (l *Legacy) Prefetch(key []byte) ([]byte, error) { panic("implement me") }
func (l *Legacy) Root() common.Hash                   { return l.root }
