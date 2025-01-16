package legacy

import (
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

var _ triedb.KVBackend = &Snapshot{}

type Snapshot struct {
	db ethdb.Database
}

func NewSnapshot(db ethdb.Database) *Snapshot {
	return &Snapshot{db: db}
}

func (s *Snapshot) Get(key []byte) ([]byte, error) {
	acc := common.BytesToHash(key[:32])
	var val []byte
	if len(key) == 32 {
		val = rawdb.ReadAccountSnapshot(s.db, acc)
	} else {
		val = rawdb.ReadStorageSnapshot(s.db, acc, common.BytesToHash(key[32:]))
	}
	return val, nil
}

func (s *Snapshot) Prefetch(key []byte) ([]byte, error) {
	return nil, nil
}

func (s *Snapshot) Update(ks, vs [][]byte) ([]byte, error) {
	for i, k := range ks {
		acc := common.BytesToHash(k[:32])
		if len(k) == 32 {
			if len(vs[i]) == 0 {
				rawdb.DeleteAccountSnapshot(s.db, acc)
			} else {
				var account types.StateAccount
				if err := rlp.DecodeBytes(vs[i], &account); err != nil {
					return nil, err
				}
				data := types.SlimAccountRLP(account)
				rawdb.WriteAccountSnapshot(s.db, acc, data)
			}
		} else {
			if len(vs[i]) == 0 {
				rawdb.DeleteStorageSnapshot(s.db, acc, common.BytesToHash(k[32:]))
			} else {
				rawdb.WriteStorageSnapshot(s.db, acc, common.BytesToHash(k[32:]), vs[i])
			}
		}
	}
	return nil, nil
}

func (s *Snapshot) Commit(root []byte) error { return nil }
func (s *Snapshot) Close() error             { return nil }
func (s *Snapshot) Root() []byte             { return nil }

func (s *Snapshot) PrefixDelete(k []byte) (int, error) {
	rawdb.DeleteAccountSnapshot(s.db, common.BytesToHash(k))

	it := s.db.NewIterator(append(rawdb.SnapshotStoragePrefix, k...), nil)
	defer it.Release()

	keysDeleted := 0
	for it.Next() {
		k := it.Key()[len(rawdb.SnapshotStoragePrefix):]
		rawdb.DeleteStorageSnapshot(s.db, common.BytesToHash(k[:32]), common.BytesToHash(k[32:]))
		keysDeleted++
	}
	if err := it.Error(); err != nil {
		return 0, err
	}
	return keysDeleted, nil
}
