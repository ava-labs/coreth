// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	defaultCommitCap  int = 10 * units.MiB
	defaultNumThreads int = 4
)

type TrieProgress struct {
	trie      *trie.StackTrie
	batch     ethdb.Batch
	batchSize int
	startFrom []byte
}

func NewTrieProgress(db ethdb.Batcher, batchSize int) *TrieProgress {
	batch := db.NewBatch()
	return &TrieProgress{
		batch:     batch,
		batchSize: batchSize,
		trie:      trie.NewStackTrie(batch),
	}
}

type StorageTrieProgress struct {
	*TrieProgress
	Account            common.Hash
	AdditionalAccounts []common.Hash
	Skipped            bool
}

// StateSyncProgress tracks the progress of syncing the main trie and the
// sub-tasks for syncing storage tries.
type StateSyncProgress struct {
	MainTrie     *TrieProgress
	MainTrieDone bool
	Root         common.Hash
	StorageTries map[common.Hash]*StorageTrieProgress
}

// stateSyncer manages the process of syncing the main trie and storage tries
// concurrently from peers, while maintaining resumability by persisting [progressMarker].
// Invariant: Each account with a corresponding entry in the snapshot and a non-empty storage root
// MUST either (a) have its storage trie fully on disk and its snapshot populated with
// the same data as the trie, or (b) have an entry in the progress marker persisted to disk.
// In case there is an entry for a storage trie in the progress marker, the in progress
// sync for that storage trie will be resumed prior to resuming the main trie sync,
// ensuring the number of tries in progress remains less than or equal to [numThreads].
// Once fewer than [numThreads] storage tries are in progress, the main trie sync will
// continue concurrently.
type stateSyncer struct {
	lock           sync.Mutex
	progressMarker *StateSyncProgress
	numThreads     int
	eta            syncETA

	syncer    *syncclient.CallbackLeafSyncer
	trieDB    *trie.Database
	db        ethdb.Database
	commitCap int
	client    syncclient.Client
}

type EVMStateSyncerConfig struct {
	Root   common.Hash
	Client syncclient.Client
	DB     ethdb.Database
}

func NewEVMStateSyncer(config *EVMStateSyncerConfig) (*stateSyncer, error) {
	progressMarker, err := loadProgress(config.DB, config.Root)
	if err != nil {
		return nil, err
	}

	// initialise tries in the progress marker
	progressMarker.MainTrie = NewTrieProgress(config.DB, defaultCommitCap)
	if err := RestoreMainTrieProgressFromSnapshot(config.DB, progressMarker.MainTrie); err != nil {
		return nil, err
	}

	for _, storageProgress := range progressMarker.StorageTries {
		storageProgress.TrieProgress = NewTrieProgress(config.DB, defaultCommitCap)
		// the first account's storage snapshot contains the key/value pairs we need to restore
		// the stack trie. if other in-progress accounts happen to share the same storage root,
		// their storage snapshot remains empty until the storage trie is fully synced, then copied
		// from the first account's storage snapshot
		if err := RestoreStorageTrieProgressFromSnapshot(config.DB, storageProgress.TrieProgress, storageProgress.Account); err != nil {
			return nil, err
		}
	}

	return &stateSyncer{
		progressMarker: progressMarker,
		commitCap:      defaultCommitCap,
		client:         config.Client,
		trieDB:         trie.NewDatabase(config.DB),
		db:             config.DB,
		numThreads:     defaultNumThreads,
		syncer:         syncclient.NewCallbackLeafSyncer(config.Client),
	}, nil
}

// Start starts the leaf syncer on the root task as well as any in-progress storage tasks.
func (s *stateSyncer) Start(ctx context.Context) {
	rootTask := &syncclient.LeafSyncTask{
		Root:          s.progressMarker.Root,
		Start:         s.progressMarker.MainTrie.startFrom,
		NodeType:      message.StateTrieNode,
		OnLeafs:       s.handleLeafs,
		OnFinish:      s.onFinish,
		OnSyncFailure: s.onSyncFailure,
	}

	storageTasks := make([]*syncclient.LeafSyncTask, 0, len(s.progressMarker.StorageTries))
	for storageRoot, storageTrieProgress := range s.progressMarker.StorageTries {
		storageTasks = append(storageTasks, &syncclient.LeafSyncTask{
			Root:          storageRoot,
			Start:         storageTrieProgress.startFrom,
			NodeType:      message.StateTrieNode,
			OnLeafs:       storageTrieProgress.handleLeafs,
			OnFinish:      s.onFinish,
			OnSyncFailure: s.onSyncFailure,
		})
	}

	s.eta.start(s.progressMarker.MainTrie.startFrom)
	s.syncer.Start(ctx, s.numThreads, rootTask, storageTasks...)
}

func (s *stateSyncer) handleLeafs(root common.Hash, keys [][]byte, values [][]byte) ([]*syncclient.LeafSyncTask, error) {
	var (
		tasks    []*syncclient.LeafSyncTask
		mainTrie = s.progressMarker.MainTrie
	)

	for i, key := range keys {
		value := values[i]
		accountHash := common.BytesToHash(key)
		if err := mainTrie.trie.TryUpdate(key, value); err != nil {
			return nil, err
		}

		// decode value into types.StateAccount
		var acc types.StateAccount
		if err := rlp.DecodeBytes(value, &acc); err != nil {
			return nil, fmt.Errorf("could not decode main trie as account, key=%s, valueLen=%d, err=%w", common.Bytes2Hex(key), len(value), err)
		}

		// check if this account has storage root that we need to fetch
		if acc.Root != (common.Hash{}) && acc.Root != types.EmptyRootHash {
			if storageTask, err := s.getStorageTrieTask(accountHash, acc.Root); err != nil {
				return nil, err
			} else if storageTask != nil {
				tasks = append(tasks, storageTask)
				if err := addInProgressTrie(s.db, acc.Root, accountHash); err != nil {
					return nil, err
				}
			}
		}

		// check if this account has code and fetch it
		codeHash := common.BytesToHash(acc.CodeHash)
		if codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash {
			codeBytes, err := s.client.GetCode(codeHash)
			if err != nil {
				return nil, fmt.Errorf("error getting code bytes for code hash [%s] from network: %w", codeHash, err)
			}
			rawdb.WriteCode(mainTrie.batch, codeHash, codeBytes)
		}

		// write account snapshot
		WriteAccountSnapshot(mainTrie.batch, accountHash, acc)

		if mainTrie.batch.ValueSize() > mainTrie.batchSize {
			if err := mainTrie.batch.Write(); err != nil {
				return nil, err
			}
			mainTrie.batch.Reset()
		}
	}
	s.eta.notifyProgress(keys[len(keys)-1])
	return tasks, nil
}

func (tp *StorageTrieProgress) handleLeafs(root common.Hash, keys [][]byte, values [][]byte) ([]*syncclient.LeafSyncTask, error) {
	// Note this method does not need to hold a lock:
	// - handleLeafs is called synchronously by CallbackLeafSyncer
	// - if additional account is encountered with the same storage trie,
	//   it will be appended to [tp.AdditionalAccounts] (not accessed here)
	for i, key := range keys {
		if err := tp.trie.TryUpdate(key, values[i]); err != nil {
			return nil, err
		}
		keyHash := common.BytesToHash(key)
		// write to [tp.Account] here, the snapshot for [tp.AdditionalAccounts] will be populated
		// after the trie is finished syncing by copying entries from [tp.Account]'s storage snapshot.
		rawdb.WriteStorageSnapshot(tp.batch, tp.Account, keyHash, values[i])
		if tp.batch.ValueSize() > tp.batchSize {
			if err := tp.batch.Write(); err != nil {
				return nil, err
			}
			tp.batch.Reset()
		}
	}
	return nil, nil // storage tries never add new tasks to the leaf syncer
}

func (s *stateSyncer) getStorageTrieTask(accountHash common.Hash, storageRoot common.Hash) (*syncclient.LeafSyncTask, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// check if we're already syncing this storage trie
	// if we are: add this account hash to the progress marker,
	// when the trie is downloaded, the snapshot will be copied
	// to this account as well
	if storageProgress, exists := s.progressMarker.StorageTries[storageRoot]; exists {
		storageProgress.AdditionalAccounts = append(storageProgress.AdditionalAccounts, accountHash)
		return nil, nil
	}

	progress := &StorageTrieProgress{
		TrieProgress: NewTrieProgress(s.db, s.commitCap),
		Account:      accountHash,
	}
	s.progressMarker.StorageTries[storageRoot] = progress
	return &syncclient.LeafSyncTask{
		Root:          storageRoot,
		NodeType:      message.StateTrieNode,
		OnLeafs:       progress.handleLeafs,
		OnFinish:      s.onFinish,
		OnSyncFailure: s.onSyncFailure,
		OnStart: func(common.Hash) (bool, error) {
			// check if this storage root is on disk
			if storageTrie, err := trie.New(storageRoot, s.trieDB); err == nil {
				// If the storage trie is already on disk, we only need to copy the storage snapshot over to [accountHash].
				// There is no need to re-sync the trie, since it is already present
				if err := WriteAccountStorageSnapshotFromTrie(s.db.NewBatch(), s.commitCap, accountHash, storageTrie); err != nil {
					// If the storage trie cannot be iterated (due to an incomplete trie from pruning this storage trie in the past)
					// then we re-sync it here. Therefore, this error is not fatal and we can safely continue here.
					log.Info("could not populate storage snapshot from trie with existing root, syncing from peers instead", "account", accountHash, "root", storageRoot, "err", err)
				} else {
					// If populating the snapshot from the existing storage trie was successful,
					// return true to skip this task
					progress.Skipped = true              // set skipped to true to avoid committing the stack trie in onFinish
					return true, s.onFinish(storageRoot) // call onFinish to delete this task from the map. onFinish will take [s.lock]
				}
			}
			return false, nil
		},
	}, nil
}

// onFinish marks the task corresponding to [root] as finished.
// If [root] is a storage root, then we remove it from the progress marker.
// when the progress marker contains no more storage root and the
// main trie is marked as complete, the main trie's root is committed (see checkAllDone).
func (s *stateSyncer) onFinish(root common.Hash) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if root == s.progressMarker.Root {
		// mark main trie as done.
		s.progressMarker.MainTrieDone = true
		return s.checkAllDone()
	}

	// is a storage trie
	storageTrieProgress, exists := s.progressMarker.StorageTries[root]
	if !exists {
		return fmt.Errorf("unknown root [%s] finished syncing", root)
	}

	if !storageTrieProgress.Skipped {
		storageRoot, err := storageTrieProgress.trie.Commit()
		if err != nil {
			return err
		}
		if storageRoot != root {
			return fmt.Errorf("unexpected storage root, expected=%s, actual=%s account=%s", root, storageRoot, storageTrieProgress.Account)
		}
	}
	// Note: we hold the lock when copying storage snapshots as well as when adding new accounts to ensure there is
	// no race condition between adding accounts and copying them.
	if len(storageTrieProgress.AdditionalAccounts) > 0 {
		// necessary to flush the batch here to write
		// any pending items to the storage snapshot before
		// we copy to other accounts.
		if err := storageTrieProgress.batch.Write(); err != nil {
			return err
		}
		storageTrieProgress.batch.Reset()
		if err := CopyStorageSnapshot(
			s.db,
			storageTrieProgress.Account,
			storageTrieProgress.batch,
			storageTrieProgress.batchSize,
			storageTrieProgress.AdditionalAccounts,
		); err != nil {
			return err
		}
	}
	delete(s.progressMarker.StorageTries, root)
	// clear the progress marker on completion of the trie
	if err := storageTrieProgress.batch.Write(); err != nil {
		return err
	}
	if err := removeInProgressStorageTrie(s.db, root, storageTrieProgress); err != nil {
		return err
	}
	s.eta.notifyTrieSynced(storageTrieProgress.Skipped)
	return s.checkAllDone()
}

// checkAllDone checks if there are no more tries in progress and the main trie is complete
// this will write the main trie's root to disk, and is the last step of stateSyncer's process.
// assumes lock is held
func (s *stateSyncer) checkAllDone() error {
	// Note: this check ensures that we do not commit the main trie until all of the storage tries
	// have been committed.
	if !s.progressMarker.MainTrieDone {
		return nil
	}
	if len(s.progressMarker.StorageTries) > 0 {
		s.eta.notifyLastStorageTries(len(s.progressMarker.StorageTries))
		return nil
	}

	mainTrie := s.progressMarker.MainTrie
	mainTrieRoot, err := mainTrie.trie.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit main trie: %w", err)
	}
	if mainTrieRoot != s.progressMarker.Root {
		return fmt.Errorf("expected main trie root [%s] not same as actual [%s]", s.progressMarker.Root, mainTrieRoot)
	}
	if err := mainTrie.batch.Write(); err != nil {
		return err
	}
	// remove the main trie storage marker, after which there should be none in the db.
	return removeInProgressTrie(s.db, mainTrieRoot, common.Hash{})
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (s *stateSyncer) Done() <-chan error { return s.syncer.Done() }

// onSyncFailure writes all in-progress batches to disk to preserve maximum progress
func (s *stateSyncer) onSyncFailure(error) error {
	for _, storageTrieProgress := range s.progressMarker.StorageTries {
		if err := storageTrieProgress.batch.Write(); err != nil {
			return err
		}
	}
	return s.progressMarker.MainTrie.batch.Write()
}
