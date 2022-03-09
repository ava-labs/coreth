// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// Database key to storage progress information
var progressKey = []byte("trieSyncProgressKey")

// ProgressMarker tracks in-progress accounts and storage roots during state sync
// Before syncing any leafs for a storage trie, Add must be called with the hash
// of the associated account and the root of the storage trie. This will persist
// the progress marker to disk.
// Account hashes must be added in lexographic order.
// When the sync for a storage trie is complete, Remove is called. This will not
// persist the progress marker to disk (next call to Add will persist this change)
// When the entire state is downloaded, Done removes the ProgressMarker from disk.
// ProgressMarker is used so upon resume, syncing the main trie can skip all accounts
// with hashes less than the first item returned by Get. (Remove will have been called
// for each of these accounts.)
type ProgressMarker struct {
	inProgress map[common.Hash]common.Hash

	lock  sync.Mutex
	codec codec.Manager
}

// Add marks the storage trie associated with [account] (at root [storageRoot]) as in-progress.
// If [account] is already being tracked then [storageRoot] must be the same as the tracked root.
// Flushes updated progress map to DB
// Returns error in case of marshal error or if tracked account root does not match [storageRoot]
func (p *ProgressMarker) Add(db ethdb.KeyValueWriter, account common.Hash, storageRoot common.Hash) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if val, exists := p.inProgress[account]; exists {
		// Should not occur. Would indicate the same account being added with a different root,
		// which is not possible in a single state trie.
		if val != storageRoot {
			log.Warn("account added with unexpected root", "account", account, "have", val, "expected", storageRoot)
			return fmt.Errorf("unexpected account root, account=%s, have=%s, expected=%s", account, val, storageRoot)
		}
		return nil
	}

	p.inProgress[account] = storageRoot

	var kvs message.SerializedMap
	for k, v := range p.inProgress {
		kvs.Keys = append(kvs.Keys, k)
		kvs.Vals = append(kvs.Vals, v)
	}

	bytes, err := p.codec.Marshal(message.Version, kvs)
	if err != nil {
		log.Error("couldn't marshal SerializedMap", "err", err)
		return err
	}
	return db.Put(progressKey, bytes)
}

// Remove removes account from in-progress map
// This is called when the storage trie associated with [account] is fully synced
func (p *ProgressMarker) Remove(account common.Hash) {
	p.lock.Lock()
	defer p.lock.Unlock()
	// Since removal of one account is followed by adding other accounts,
	// we don't write the map on removal to disk.
	delete(p.inProgress, account)
}

// Done deletes the progressKey marking all leafs as having synced
func (p *ProgressMarker) Done(db ethdb.KeyValueWriter) error {
	p.Reset()
	return db.Delete(progressKey)
}

// Get returns list of accounts and their storage roots that were in progress from disk
// This function is called when Syncer is initialised. Sync resumes from returned accounts
// and storage roots
// Account (hashes) are sorted in increasing lexographic order and storage roots are returned
// in corresponding order.
func (p *ProgressMarker) Get() (sortedAccounts []common.Hash, storageRoots []common.Hash) {
	p.lock.Lock()
	defer p.lock.Unlock()

	sortedAccounts = make([]common.Hash, 0, len(p.inProgress))
	for k := range p.inProgress {
		sortedAccounts = append(sortedAccounts, k)
	}
	sort.Slice(sortedAccounts, func(i, j int) bool { return bytes.Compare(sortedAccounts[i][:], sortedAccounts[j][:]) < 0 })
	for _, k := range sortedAccounts {
		storageRoots = append(storageRoots, p.inProgress[k])
	}
	return sortedAccounts, storageRoots
}

// Reset clears the state
func (p *ProgressMarker) Reset() {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.inProgress = make(map[common.Hash]common.Hash)
}

// NewProgressMarker creates a new [ProgressMarker] used to track the progress of
// syncing accounts and their storage tries. If a previously stored marker is
// found in [db], the returned [ProgressMarker] will be initialized with the contents
// from disk.
func NewProgressMarker(db ethdb.KeyValueReader, codec codec.Manager, numThreads uint) (*ProgressMarker, error) {
	inProgress := make(map[common.Hash]common.Hash, numThreads)
	has, err := db.Has(progressKey)
	if err != nil {
		return nil, err
	}

	if has {
		val, err := db.Get(progressKey)
		if err != nil {
			return nil, err
		}
		var kvs message.SerializedMap
		if _, err := codec.Unmarshal(val, &kvs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal progress marker from DB: %w", err)
		}
		if len(kvs.Keys) != len(kvs.Vals) {
			return nil, fmt.Errorf("unexpected mismatch between number of serialized keys (%d) and vals (%d)", len(kvs.Keys), len(kvs.Vals))
		}
		for i, k := range kvs.Keys {
			inProgress[k] = kvs.Vals[i]
		}
	}

	return &ProgressMarker{
		inProgress: inProgress,
		codec:      codec,
	}, nil
}
