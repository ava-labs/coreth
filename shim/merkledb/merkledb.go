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

func (m *MerkleDB) Update(batch triedb.Batch) (common.Hash, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	ctx := context.TODO()
	changes := make([]database.BatchOp, len(batch))
	for i, kv := range batch {
		changes[i] = database.BatchOp{
			Key:    kv.Key,
			Value:  kv.Value,
			Delete: len(kv.Value) == 0,
		}
	}
	view, err := m.latestViewLocked().NewView(ctx, merkledb.ViewChanges{BatchOps: changes})
	if err != nil {
		return common.Hash{}, err
	}
	root, err := view.GetMerkleRoot(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	m.pendingViews = append(m.pendingViews, view)
	m.pendingViewRoots = append(m.pendingViewRoots, common.Hash(root))
	return common.Hash(root), nil
}

func (m *MerkleDB) Commit(root common.Hash) error {
	m.lock.Lock()
	defer m.lock.Unlock()

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

func (m *MerkleDB) Root() common.Hash {
	ctx := context.TODO()
	root, err := m.latestView().GetMerkleRoot(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get merkle root: %v", err))
	}
	return common.Hash(root)
}
