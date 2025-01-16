package legacy

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

var _ triedb.KVBackend = &Legacy{}

type Legacy struct {
	triedb            *triedb.Database
	root              common.Hash
	count             uint64
	dereference       bool
	trackDeletedTries ethdb.KeyValueStore
}

func New(triedb *triedb.Database, root common.Hash, count uint64, dereference bool) *Legacy {
	return &Legacy{
		triedb:      triedb,
		root:        root,
		count:       count,
		dereference: dereference,
	}
}

func (l *Legacy) TrackDeletedTries(db ethdb.KeyValueStore) {
	l.trackDeletedTries = db
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

func (l *Legacy) Update(ks, vs [][]byte) ([]byte, error) {
	// Collect all nodes that are modified during the update
	// Defined here so we can process storage deletes
	nodes := trienode.NewMergedNodeSet()

	accounts, err := trie.New(trie.StateTrieID(l.root), l.triedb)
	if err != nil {
		return nil, err
	}
	// Process the storage tries first, this means we can access the root for the
	// storage tries before they are updated in the account trie. Necessary for
	// the hash scheme.
	tries := make(map[common.Hash]*trie.Trie)
	for i, k := range ks {
		v := vs[i]
		accHash := common.BytesToHash(k[:32])
		if len(k) == 32 {
			if len(v) == 0 {
				prevRoot, err := getAccountRoot(accounts, accHash)
				if err != nil {
					return nil, fmt.Errorf("failed to get account root %x: %w", accHash, err)
				}
				if prevRoot != types.EmptyRootHash {
					return nil, fmt.Errorf("account %x is deleted but has non-empty storage trie", accHash)
				}
				if _, ok := tries[accHash]; ok {
					return nil, fmt.Errorf("account %x is deleted but has pending storage trie", accHash)
				}
			}

			// otherwise, skip account updates for now
			continue
		}

		tr, ok := tries[accHash]
		if !ok {
			if l.trackDeletedTries != nil {
				found, err := l.trackDeletedTries.Has(accHash[:])
				if err != nil {
					return nil, fmt.Errorf("failed to check if account %x is deleted: %w", accHash, err)
				}
				if found {
					got, err := l.trackDeletedTries.Get(accHash[:])
					if err != nil {
						return nil, fmt.Errorf("failed to get deleted trie %x: %w", accHash, err)
					}
					fmt.Println("::: found deleted trie", accHash, binary.BigEndian.Uint64(got), l.count, i)
					return nil, fmt.Errorf("account %x is deleted", accHash)
				}
			}
			root, err := getAccountRoot(accounts, accHash)
			if err != nil {
				return nil, err
			}
			tr, err = trie.New(trie.StorageTrieID(l.root, accHash, root), l.triedb)
			if err != nil {
				return nil, fmt.Errorf("failed to create storage trie %x: %w", accHash, err)
			}
			tries[accHash] = tr
		}

		// Update the storage trie
		if len(v) == 0 {
			tr.MustDelete(k[32:])
		} else {
			tr.MustUpdate(k[32:], v)
		}
	}

	// Hash the storage tries
	for _, tr := range tries {
		_, set, err := tr.Commit(false)
		if err != nil {
			return nil, err
		}
		if set != nil {
			nodes.Merge(set)
		}
	}

	// Update the account trie
	for i, k := range ks {
		v := vs[i]
		if len(k) == 64 {
			continue
		}
		if len(v) == 0 {
			accounts.MustDelete(k)
		} else {
			accounts.MustUpdate(k, v)
		}
	}

	// Verify account trie updates match the storage trie updates
	for accHash, tr := range tries {
		root, err := getAccountRoot(accounts, accHash)
		if err != nil {
			return nil, err
		}
		if root != tr.Hash() {
			return nil, fmt.Errorf("account %x trie root mismatch (%x != %x)", accHash, root, tr.Hash())
		}
	}

	next, set, err := accounts.Commit(true)
	if err != nil {
		return nil, err
	}
	if set != nil {
		nodes.Merge(set)
	}

	if err := l.triedb.Update(next, l.root, l.count, nodes, nil); err != nil {
		return nil, err
	}

	// TODO: fix hashdb scheme later
	l.root = next
	l.count++
	return next[:], nil
}

func (l *Legacy) Commit(rootBytes []byte) error {
	root := common.BytesToHash(rootBytes)
	return l.triedb.Commit(root, false)
}
func (l *Legacy) Close() error                        { return nil }
func (l *Legacy) Get(key []byte) ([]byte, error)      { panic("implement me") }
func (l *Legacy) Prefetch(key []byte) ([]byte, error) { panic("implement me") }
func (l *Legacy) Root() []byte                        { return l.root[:] }

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
) (bool, int, map[common.Hash][]byte, *trienode.NodeSet, error) {
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
		leafs int
		nodes = trienode.NewNodeSet(addrHash)
		slots = make(map[common.Hash][]byte)
	)
	for it.Next(true) {
		if size > storageDeleteLimit {
			return true, leafs, nil, nil, nil
		}
		if it.Leaf() {
			slots[common.BytesToHash(it.LeafKey())] = common.CopyBytes(it.LeafBlob())
			size += common.StorageSize(common.HashLength + len(it.LeafBlob()))
			leafs++
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
	return false, leafs, slots, nodes, nil
}

func (l *Legacy) PrefixDelete(prefix []byte) (int, error) {
	if l.trackDeletedTries != nil {
		if err := l.trackDeletedTries.Put(prefix, binary.BigEndian.AppendUint64(nil, l.count)); err != nil {
			return 0, fmt.Errorf("failed to track deleted trie %x: %w", prefix, err)
		}
	}

	accounts, err := trie.New(trie.StateTrieID(l.root), l.triedb)
	if err != nil {
		return 0, err
	}
	origin, err := getAccountRoot(accounts, common.BytesToHash(prefix))
	if err != nil {
		return 0, err
	}
	if origin == types.EmptyRootHash {
		return 0, nil
	}
	nodes := trienode.NewMergedNodeSet()
	_, leafs, _, set, err := slowDeleteStorage(l.triedb, l.root, common.BytesToHash(prefix), origin)
	if err != nil {
		return 0, err
	}
	if set != nil {
		nodes.Merge(set)
	}
	accounts.MustDelete(prefix)
	next, set, err := accounts.Commit(true)
	if err != nil {
		return 0, err
	}
	if set != nil {
		nodes.Merge(set)
	}
	if err := l.triedb.Update(next, l.root, l.count, nodes, nil); err != nil {
		return 0, err
	}
	l.root = next
	l.count++
	return leafs, nil
}
