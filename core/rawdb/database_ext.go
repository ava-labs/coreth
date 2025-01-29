// (c) 2019-2025, Ava Labs, Inc.
package rawdb

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
)

// ClearPrefix removes all keys in db that begin with prefix and match an
// expected key length. [keyLen] should include the length of the prefix.
func ClearPrefix(db ethdb.KeyValueStore, prefix []byte, keyLen int) error {
	it := db.NewIterator(prefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		key := common.CopyBytes(it.Key())
		if len(key) != keyLen {
			// avoid deleting keys that do not match the expected length
			continue
		}
		if err := batch.Delete(key); err != nil {
			return err
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}
