package merkledb

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
)

var _ triedb.KVBackend = &MerkleDB{}

type MerkleDB struct {
	lock             sync.RWMutex
	db               merkledb.MerkleDB
	pendingViews     []merkledb.View
	pendingViewRoots []common.Hash
}

func NewMerkleDB(db merkledb.MerkleDB) *MerkleDB {
	return &MerkleDB{db: db}
}

func (m *MerkleDB) Get(key []byte) ([]byte, error) {
	val, err := m.latestView().GetValue(context.TODO(), key)
	if err == database.ErrNotFound {
		return nil, nil
	}
	return val, err
}

func (m *MerkleDB) Prefetch(key []byte) ([]byte, error) {
	return nil, m.db.PrefetchPath(key)
}

func (m *MerkleDB) latestView() merkledb.Trie {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.latestViewLocked()
}

func (m *MerkleDB) latestViewLocked() merkledb.Trie {
	if len(m.pendingViews) == 0 {
		return m.db
	}
	return m.pendingViews[len(m.pendingViews)-1]
}

func (m *MerkleDB) Update(ks, vs [][]byte) ([]byte, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	ctx := context.TODO()
	changes := make([]database.BatchOp, len(ks))
	for i, k := range ks {
		v := vs[i]
		changes[i] = database.BatchOp{Key: k, Value: v, Delete: len(v) == 0}
	}
	view, err := m.latestViewLocked().NewView(ctx, merkledb.ViewChanges{BatchOps: changes})
	if err != nil {
		return nil, err
	}
	root, err := view.GetMerkleRoot(ctx)
	if err != nil {
		return nil, err
	}
	m.pendingViews = append(m.pendingViews, view)
	m.pendingViewRoots = append(m.pendingViewRoots, common.Hash(root))
	return root[:], nil
}

func (m *MerkleDB) Commit(rootBytes []byte) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	root := common.BytesToHash(rootBytes)

	if len(m.pendingViews) == 0 {
		return fmt.Errorf("no pending views")
	}
	pendingRootIdx := -1
	for i, pendingRoot := range m.pendingViewRoots {
		if pendingRoot == root {
			pendingRootIdx = i
			break
		}
	}
	if pendingRootIdx > 0 {
		for i := 0; i < pendingRootIdx; i++ {
			if err := m.commitToDisk(m.pendingViewRoots[0]); err != nil {
				return err
			}
		}
	}
	return m.commitToDisk(root)
}

func (m *MerkleDB) commitToDisk(root common.Hash) error {
	if m.pendingViewRoots[0] != root {
		return fmt.Errorf("root mismatch: expected %x, got %x", root, m.pendingViewRoots[0])
	}
	ctx := context.TODO()
	if err := m.pendingViews[0].CommitToDB(ctx); err != nil {
		return err
	}
	m.pendingViews = m.pendingViews[1:]
	m.pendingViewRoots = m.pendingViewRoots[1:]
	fmt.Printf("Commit: %x\n", root)
	return nil
}

func (m *MerkleDB) Root() []byte {
	ctx := context.TODO()
	root, err := m.latestView().GetMerkleRoot(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get merkle root: %v", err))
	}
	return root[:]
}

func (m *MerkleDB) Close() error {
	last := common.Hash{}
	m.lock.Lock()
	if len(m.pendingViewRoots) > 0 {
		last = m.pendingViewRoots[len(m.pendingViewRoots)-1]
	}
	m.lock.Unlock()

	if last != (common.Hash{}) {
		if err := m.Commit(last[:]); err != nil {
			return err
		}
	}

	return m.db.Close()
}

func (m *MerkleDB) PrefixDelete(prefix []byte) (int, error) {
	return 0, nil
}
