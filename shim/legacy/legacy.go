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
	// Collect all nodes that are modified during the update
	// Defined here so we can process storage deletes
	nodes := trienode.NewMergedNodeSet()

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
				// this trie is DELETED. First we will remove it from storage:
				// if it was updated before, we should have a trie for it
				prevRoot, err := getAccountRoot(accounts, accHash)
				if err != nil {
					return common.Hash{}, fmt.Errorf("failed to get account root %x: %w", accHash, err)
				}
				if prevRoot != types.EmptyRootHash {
					fmt.Printf(":: slow delete storage for %x\n", accHash)
					_, _, _, set, err := slowDeleteStorage(l.triedb, l.root, accHash, prevRoot)
					if err != nil {
						return common.Hash{}, err
					}
					if set != nil {
						updates, deletes := set.Size()
						fmt.Printf("::: set merged: %d updates, %d deletes\n", updates, deletes)
						nodes.Merge(set)
					}
				}

				// Also any pending updates should not be apply
				// Further updates shold apply to an empty trie
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

const (
	// storageDeleteLimit denotes the highest permissible memory allocation
	// employed for contract storage deletion.
	storageDeleteLimit = 512 * 1024 * 1024
)

// slowDeleteStorage serves as a less-efficient alternative to "fastDeleteStorage,"
// employed when the associated state snapshot is not available. It iterates the
// storage slots along with all internal trie nodes via trie directly.
func slowDeleteStorage(
	db *triedb.Database, originalRoot, addrHash, root common.Hash,
) (bool, common.StorageSize, map[common.Hash][]byte, *trienode.NodeSet, error) {
	tr, err := trie.New(trie.StorageTrieID(originalRoot, addrHash, root), db)
	if err != nil {
		return false, 0, nil, nil, fmt.Errorf("failed to open storage trie, err: %w", err)
	}
	it, err := tr.NodeIterator(nil)
	if err != nil {
		return false, 0, nil, nil, fmt.Errorf("failed to open storage iterator, err: %w", err)
	}
	var (
		size  common.StorageSize
		nodes = trienode.NewNodeSet(addrHash)
		slots = make(map[common.Hash][]byte)
	)
	for it.Next(true) {
		if size > storageDeleteLimit {
			return true, size, nil, nil, nil
		}
		if it.Leaf() {
			slots[common.BytesToHash(it.LeafKey())] = common.CopyBytes(it.LeafBlob())
			size += common.StorageSize(common.HashLength + len(it.LeafBlob()))
			continue
		}
		if it.Hash() == (common.Hash{}) {
			continue
		}
		size += common.StorageSize(len(it.Path()))
		nodes.AddNode(it.Path(), trienode.NewDeleted())
	}
	if err := it.Error(); err != nil {
		return false, 0, nil, nil, err
	}
	return false, size, slots, nodes, nil
}
