// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
)

var (
	_ Syncer                  = &atomicSyncer{}
	_ syncclient.LeafSyncTask = &atomicSyncerLeafTask{}
)

const (
	// AtomicTrieNode represents a leaf node that belongs to the atomic trie.
	AtomicTrieNode message.NodeType = 2
)

// AtomicTrie maintains an index of atomic operations by blockchainIDs for every block
// height containing atomic transactions. The backing data structure for this index is
// a Trie. The keys of the trie are block heights and the values (leaf nodes)
// are the atomic operations applied to shared memory while processing the block accepted
// at the corresponding height.
type AtomicTrie interface {
	// OpenTrie returns a modifiable instance of the atomic trie backed by trieDB
	// opened at hash.
	OpenTrie(hash common.Hash) (*trie.Trie, error)

	// LastCommitted returns the last committed hash and corresponding block height
	LastCommitted() (common.Hash, uint64)

	// Root returns hash if it exists at specified height
	// if trie was not committed at provided height, it returns
	// common.Hash{} instead
	Root(height uint64) (common.Hash, error)

	// InsertTrie updates the trieDB with the provided node set and adds a reference
	// to root in the trieDB. Once InsertTrie is called, it is expected either
	// AcceptTrie or RejectTrie be called for the same root.
	InsertTrie(nodes *trienode.NodeSet, root common.Hash) error

	// AcceptTrie marks root as the last accepted atomic trie root, and
	// commits the trie to persistent storage if height is divisible by
	// the commit interval. Returns true if the trie was committed.
	AcceptTrie(height uint64, root common.Hash) (bool, error)
}

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	Start(ctx context.Context) error
	Done() <-chan error
}

// atomicSyncer is used to sync the atomic trie from the network. The CallbackLeafSyncer
// is responsible for orchestrating the sync while atomicSyncer is responsible for maintaining
// the state of progress and writing the actual atomic trie to the trieDB.
type atomicSyncer struct {
	db           *versiondb.Database
	atomicTrie   AtomicTrie
	trie         *trie.Trie // used to update the atomic trie
	targetRoot   common.Hash
	targetHeight uint64

	// syncer is used to sync leaves from the network.
	syncer *syncclient.CallbackLeafSyncer

	// lastHeight is the greatest height for which key / values
	// were last inserted into the [atomicTrie]
	lastHeight uint64
}

// addZeros adds [common.HashLenth] zeros to [height] and returns the result as []byte
func addZeroes(height uint64) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, state.AtomicTrieKeyLength)}
	packer.PackLong(height)
	packer.PackFixedBytes(bytes.Repeat([]byte{0x00}, common.HashLength))
	return packer.Bytes
}

func NewAtomicSyncer(client syncclient.LeafClient, vdb *versiondb.Database, atomicTrie AtomicTrie, targetRoot common.Hash, targetHeight uint64, requestSize uint16) (*atomicSyncer, error) {
	lastCommittedRoot, lastCommit := atomicTrie.LastCommitted()
	trie, err := atomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return nil, err
	}

	atomicSyncer := &atomicSyncer{
		db:           vdb,
		atomicTrie:   atomicTrie,
		trie:         trie,
		targetRoot:   targetRoot,
		targetHeight: targetHeight,
		lastHeight:   lastCommit,
	}
	tasks := make(chan syncclient.LeafSyncTask, 1)
	tasks <- &atomicSyncerLeafTask{atomicSyncer: atomicSyncer}
	close(tasks)
	atomicSyncer.syncer = syncclient.NewCallbackLeafSyncer(client, tasks, requestSize)
	return atomicSyncer, nil
}

// Start begins syncing the target atomic root.
func (s *atomicSyncer) Start(ctx context.Context) error {
	s.syncer.Start(ctx, 1, s.onSyncFailure)
	return nil
}

// onLeafs is the callback for the leaf syncer, which will insert the key-value pairs into the trie.
func (s *atomicSyncer) onLeafs(keys [][]byte, values [][]byte) error {
	for i, key := range keys {
		if len(key) != state.AtomicTrieKeyLength {
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
func (s *atomicSyncer) onFinish() error {
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
func (s *atomicSyncer) onSyncFailure(error) error {
	return nil
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (s *atomicSyncer) Done() <-chan error { return s.syncer.Done() }

type atomicSyncerLeafTask struct {
	atomicSyncer *atomicSyncer
}

func (a *atomicSyncerLeafTask) Start() []byte                  { return addZeroes(a.atomicSyncer.lastHeight + 1) }
func (a *atomicSyncerLeafTask) End() []byte                    { return nil }
func (a *atomicSyncerLeafTask) NodeType() message.NodeType     { return AtomicTrieNode }
func (a *atomicSyncerLeafTask) OnFinish(context.Context) error { return a.atomicSyncer.onFinish() }
func (a *atomicSyncerLeafTask) OnStart() (bool, error)         { return false, nil }
func (a *atomicSyncerLeafTask) Root() common.Hash              { return a.atomicSyncer.targetRoot }
func (a *atomicSyncerLeafTask) Account() common.Hash           { return common.Hash{} }
func (a *atomicSyncerLeafTask) OnLeafs(keys, vals [][]byte) error {
	return a.atomicSyncer.onLeafs(keys, vals)
}
