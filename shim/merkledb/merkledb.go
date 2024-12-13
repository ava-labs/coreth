package merkledb

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
)

var _ triedb.KVBackend = &MerkleDB{}

type MerkleDB struct {
	db              merkledb.MerkleDB
	pendingView     merkledb.View
	pendingViewRoot common.Hash
}

func NewMerkleDB(db merkledb.MerkleDB) *MerkleDB {
	return &MerkleDB{db: db}
}

func (m *MerkleDB) Get(key []byte) ([]byte, error) {
	val, err := m.db.Get(key)
	if err == database.ErrNotFound {
		return nil, nil
	}
	return val, err
}

func (m *MerkleDB) Update(batch triedb.Batch) (common.Hash, error) {
	ctx := context.TODO()
	changes := make([]database.BatchOp, len(batch))
	for i, kv := range batch {
		changes[i] = database.BatchOp{
			Key:    kv.Key,
			Value:  kv.Value,
			Delete: len(kv.Value) == 0,
		}
	}
	view, err := m.db.NewView(ctx, merkledb.ViewChanges{BatchOps: changes})
	if err != nil {
		return common.Hash{}, err
	}
	root, err := view.GetMerkleRoot(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	m.pendingView = view
	m.pendingViewRoot = common.Hash(root)
	return common.Hash(root), nil
}

func (m *MerkleDB) Commit(root common.Hash) error {
	if m.pendingViewRoot != root {
		return fmt.Errorf("root mismatch: expected %x, got %x", root, m.pendingViewRoot)
	}
	ctx := context.TODO()
	if err := m.pendingView.CommitToDB(ctx); err != nil {
		return err
	}
	m.pendingView = nil
	m.pendingViewRoot = common.Hash{}
	fmt.Printf("Commit: %x\n", root)
	return nil
}

func (m *MerkleDB) Root() common.Hash {
	ctx := context.TODO()
	root, err := m.db.GetMerkleRoot(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get merkle root: %v", err))
	}
	return common.Hash(root)
}
