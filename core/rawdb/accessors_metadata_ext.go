// (c) 2019-2025, Ava Labs, Inc.
package rawdb

import (
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
)

// WriteTimeMarker writes a marker of the current time in the db at [key]
func WriteTimeMarker(db ethdb.KeyValueStore, key []byte) error {
	data, err := rlp.EncodeToBytes(uint64(time.Now().Unix()))
	if err != nil {
		return err
	}
	return db.Put(key, data)
}

// ReadTimeMarker reads the timestamp stored at [key]
func ReadTimeMarker(db ethdb.KeyValueStore, key []byte) (time.Time, error) {
	data, err := db.Get(key)
	if err != nil {
		return time.Time{}, err
	}

	var lastRun uint64
	if err := rlp.DecodeBytes(data, &lastRun); err != nil {
		return time.Time{}, err
	}

	return time.Unix(int64(lastRun), 0), nil
}

// DeleteTimeMarker deletes any value stored at [key]
func DeleteTimeMarker(db ethdb.KeyValueStore, key []byte) error {
	return db.Delete(key)
}

// WriteOfflinePruning writes a marker of the last attempt to run offline pruning
// The marker is written when offline pruning completes and is deleted when the node
// is started successfully with offline pruning disabled. This ensures users must
// disable offline pruning and start their node successfully between runs of offline
// pruning.
func WriteOfflinePruning(db ethdb.KeyValueStore) error {
	return WriteTimeMarker(db, offlinePruningKey)
}

// ReadOfflinePruning reads the most recent timestamp of an attempt to run offline
// pruning if present.
func ReadOfflinePruning(db ethdb.KeyValueStore) (time.Time, error) {
	return ReadTimeMarker(db, offlinePruningKey)
}

// DeleteOfflinePruning deletes any marker of the last attempt to run offline pruning.
func DeleteOfflinePruning(db ethdb.KeyValueStore) error {
	return DeleteTimeMarker(db, offlinePruningKey)
}

// WritePopulateMissingTries writes a marker for the current attempt to populate
// missing tries.
func WritePopulateMissingTries(db ethdb.KeyValueStore) error {
	return WriteTimeMarker(db, populateMissingTriesKey)
}

// ReadPopulateMissingTries reads the most recent timestamp of an attempt to
// re-populate missing trie nodes.
func ReadPopulateMissingTries(db ethdb.KeyValueStore) (time.Time, error) {
	return ReadTimeMarker(db, populateMissingTriesKey)
}

// DeletePopulateMissingTries deletes any marker of the last attempt to
// re-populate missing trie nodes.
func DeletePopulateMissingTries(db ethdb.KeyValueStore) error {
	return DeleteTimeMarker(db, populateMissingTriesKey)
}

// WritePruningDisabled writes a marker to track whether the node has ever run
// with pruning disabled.
func WritePruningDisabled(db ethdb.KeyValueStore) error {
	return db.Put(pruningDisabledKey, nil)
}

// HasPruningDisabled returns true if there is a marker present indicating that
// the node has run with pruning disabled at some pooint.
func HasPruningDisabled(db ethdb.KeyValueStore) (bool, error) {
	return db.Has(pruningDisabledKey)
}

// WriteAcceptorTip writes [hash] as the last accepted block that has been fully processed.
func WriteAcceptorTip(db ethdb.KeyValueWriter, hash common.Hash) error {
	return db.Put(acceptorTipKey, hash[:])
}

// ReadAcceptorTip reads the hash of the last accepted block that was fully processed.
// If there is no value present (the index is being initialized for the first time), then the
// empty hash is returned.
func ReadAcceptorTip(db ethdb.KeyValueReader) (common.Hash, error) {
	has, err := db.Has(acceptorTipKey)
	// If the index is not present on disk, the [acceptorTipKey] index has not been initialized yet.
	if !has || err != nil {
		return common.Hash{}, err
	}
	h, err := db.Get(acceptorTipKey)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(h), nil
}
