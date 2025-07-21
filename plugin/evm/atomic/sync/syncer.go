// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"runtime"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/libevm/common"

	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"

	"github.com/ava-labs/libevm/trie"
)

// TrieNode represents a leaf node that belongs to the atomic trie.
const TrieNode message.NodeType = 2

const (
	// MinNumWorkers is the minimum number of worker goroutines to use for atomic trie syncing.
	MinNumWorkers = 1
)

// MaxNumWorkers returns the maximum number of worker goroutines to use for atomic trie syncing.
// For I/O bound work like network syncing, we can be more aggressive than CPU-bound work.
// This allows up to 2x CPU cores for I/O bound work, but caps at 64 for very large systems.
// Rationale:
// - I/O bound work benefits from more goroutines than CPU cores.
// - 2x CPU cores provides good parallelism without overwhelming the system.
// - Cap of 64 prevents excessive resource usage on very large systems.
func MaxNumWorkers() int {
	cpus := runtime.NumCPU()

	return min(cpus*2, 64)
}

// DefaultNumWorkers returns the optimal number of worker goroutines for atomic trie syncing
// based on available CPU cores, with sensible bounds.
// Note: These are goroutines, not OS threads. The Go runtime scheduler will distribute
// them efficiently across the available OS threads (GOMAXPROCS).
// Rationale:
// - 75% of CPU cores provides good parallelism while leaving headroom for other operations.
// - This balances performance with system resource usage.
// - Bounded by MinNumWorkers and MaxNumWorkers for safety.
func DefaultNumWorkers() int {
	cpus := runtime.NumCPU()

	// Use 75% of available CPUs to leave some headroom for other operations.
	optimal := (cpus * 3) / 4

	// Apply bounds.
	if optimal < MinNumWorkers {
		return MinNumWorkers
	}

	if optimal > MaxNumWorkers() {
		return MaxNumWorkers()
	}

	return optimal
}

var (
	_ Syncer                  = (*syncer)(nil)
	_ syncclient.LeafSyncTask = (*syncerLeafTask)(nil)
)

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	Start(ctx context.Context, numWorkers ...int) error
	Done() <-chan error
	Wait(ctx context.Context) error
}

// syncer is used to sync the atomic trie from the network. The CallbackLeafSyncer
// is responsible for orchestrating the sync while syncer is responsible for maintaining
// the state of progress and writing the actual atomic trie to the trieDB.
type syncer struct {
	db           *versiondb.Database
	atomicTrie   *atomicstate.AtomicTrie
	trie         *trie.Trie // used to update the atomic trie
	targetRoot   common.Hash
	targetHeight uint64

	// syncer is used to sync leaves from the network.
	syncer *syncclient.CallbackLeafSyncer

	// lastHeight is the greatest height for which key / values
	// were last inserted into the [atomicTrie]
	lastHeight uint64

	// cancel is used to cancel the sync operation
	cancel context.CancelFunc
}

// addZeros adds [common.HashLenth] zeros to [height] and returns the result as []byte
func addZeroes(height uint64) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, atomicstate.TrieKeyLength)}
	packer.PackLong(height)
	packer.PackFixedBytes(bytes.Repeat([]byte{0x00}, common.HashLength))
	return packer.Bytes
}

// newSyncer returns a new syncer instance that will sync the atomic trie from the network.
func newSyncer(client syncclient.LeafClient, vdb *versiondb.Database, atomicTrie *atomicstate.AtomicTrie, targetRoot common.Hash, targetHeight uint64, requestSize uint16, numWorkers int) (*syncer, error) {
	lastCommittedRoot, lastCommit := atomicTrie.LastCommitted()
	trie, err := atomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return nil, err
	}

	syncer := &syncer{
		db:           vdb,
		atomicTrie:   atomicTrie,
		trie:         trie,
		targetRoot:   targetRoot,
		targetHeight: targetHeight,
		lastHeight:   lastCommit,
	}

	// Create tasks channel with capacity for the number of workers.
	tasks := make(chan syncclient.LeafSyncTask, numWorkers)

	// For atomic trie syncing, we typically want a single task since the trie is sequential.
	// But we can create multiple tasks if needed for parallel processing of different ranges
	tasks <- &syncerLeafTask{syncer: syncer}
	close(tasks)

	syncer.syncer = syncclient.NewCallbackLeafSyncer(client, tasks, requestSize)
	return syncer, nil
}

// Start begins syncing the target atomic root with the specified number of worker goroutines.
// If numWorkers is not provided, it defaults to DefaultNumWorkers().
// Note: numWorkers refers to worker goroutines, not OS threads. The Go runtime
// scheduler will efficiently distribute these goroutines across available OS threads.
func (s *syncer) Start(ctx context.Context, numWorkers ...int) error {
	workers := DefaultNumWorkers()
	if len(numWorkers) > 0 {
		workers = numWorkers[0]

		// Validate worker count.
		if workers < MinNumWorkers {
			return fmt.Errorf("numWorkers (%d) must be at least %d", workers, MinNumWorkers)
		}

		if workers > MaxNumWorkers() {
			return fmt.Errorf("numWorkers (%d) must be at most %d", workers, MaxNumWorkers())
		}
	}

	ctx, s.cancel = context.WithCancel(ctx)
	s.syncer.Start(ctx, workers, s.onSyncFailure)

	return nil
}

// onLeafs is the callback for the leaf syncer, which will insert the key-value pairs into the trie.
func (s *syncer) onLeafs(keys [][]byte, values [][]byte) error {
	for i, key := range keys {
		if len(key) != atomicstate.TrieKeyLength {
			return fmt.Errorf("unexpected key len (%d) in atomic trie sync", len(key))
		}
		// key = height + blockchainID
		height := binary.BigEndian.Uint64(key[:wrappers.LongLen])
		if height > s.lastHeight {
			// If this key belongs to a new height, we commit
			// the trie at the previous height before adding this key.
			root, nodes, err := s.trie.Commit(false)
			if err != nil {
				return err
			}
			if err := s.atomicTrie.InsertTrie(nodes, root); err != nil {
				return err
			}
			// AcceptTrie commits the trieDB and returns [isCommit] as true
			// if we have reached or crossed a commit interval.
			isCommit, err := s.atomicTrie.AcceptTrie(s.lastHeight, root)
			if err != nil {
				return err
			}
			if isCommit {
				// Flush pending changes to disk to preserve progress and
				// free up memory if the trieDB was committed.
				if err := s.db.Commit(); err != nil {
					return err
				}
			}
			// Trie must be re-opened after committing (not safe for re-use after commit)
			trie, err := s.atomicTrie.OpenTrie(root)
			if err != nil {
				return err
			}
			s.trie = trie
			s.lastHeight = height
		}

		if err := s.trie.Update(key, values[i]); err != nil {
			return err
		}
	}
	return nil
}

// onFinish is called when sync for this trie is complete.
// commit the trie to disk and perform the final checks that we synced the target root correctly.
func (s *syncer) onFinish() error {
	// commit the trie on finish
	root, nodes, err := s.trie.Commit(false)
	if err != nil {
		return err
	}
	if err := s.atomicTrie.InsertTrie(nodes, root); err != nil {
		return err
	}
	if _, err := s.atomicTrie.AcceptTrie(s.targetHeight, root); err != nil {
		return err
	}
	if err := s.db.Commit(); err != nil {
		return err
	}

	// the root of the trie should always match the targetRoot  since we already verified the proofs,
	// here we check the root mainly for correctness of the atomicTrie's pointers and it should never fail.
	if s.targetRoot != root {
		return fmt.Errorf("synced root (%s) does not match expected (%s) for atomic trie ", root, s.targetRoot)
	}
	return nil
}

// onSyncFailure is a no-op since we flush progress to disk at the regular commit interval when syncing
// the atomic trie.
func (s *syncer) onSyncFailure(error) error {
	return nil
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (s *syncer) Done() <-chan error { return s.syncer.Done() }

// Wait blocks until the sync operation completes and returns any error that occurred.
// It respects context cancellation and returns ctx.Err() if the context is cancelled.
// Note: This method should only be called after Start() has been called.
func (s *syncer) Wait(ctx context.Context) error {
	select {
	case err := <-s.syncer.Done():
		return err
	case <-ctx.Done():
		// Only cancel if we have a cancel function (i.e., Start() was called).
		if s.cancel != nil {
			s.cancel()
		}
		return ctx.Err()
	}
}

type syncerLeafTask struct {
	syncer *syncer
}

func (a *syncerLeafTask) Start() []byte                  { return addZeroes(a.syncer.lastHeight + 1) }
func (a *syncerLeafTask) End() []byte                    { return nil }
func (a *syncerLeafTask) NodeType() message.NodeType     { return TrieNode }
func (a *syncerLeafTask) OnFinish(context.Context) error { return a.syncer.onFinish() }
func (a *syncerLeafTask) OnStart() (bool, error)         { return false, nil }
func (a *syncerLeafTask) Root() common.Hash              { return a.syncer.targetRoot }
func (a *syncerLeafTask) Account() common.Hash           { return common.Hash{} }
func (a *syncerLeafTask) OnLeafs(keys, vals [][]byte) error {
	return a.syncer.onLeafs(keys, vals)
}
