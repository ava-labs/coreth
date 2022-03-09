// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var _ Syncer = &leafSyncer{}

// Syncer allows fetching a trie at the given root,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	// Start begins syncing at root, honors [ctx] deadline/cancel.
	Start(ctx context.Context, root common.Hash)
	Error() error
	Done() chan struct{}
}

// leafSyncer implements the Syncer interface and
// fetches a trie by issuing GetLeaf requests to
// the [client].
// leafSyncer supports multiple threads (each to
// fetch one trie at a given root, used to fetch
// account storage in parallel) and a progress marker
// that tracks which storage tries have been fetched.
type leafSyncer struct {
	// work
	nodeType message.NodeType
	keyLen   int
	root     common.Hash

	// database
	db        ethdb.Database
	batch     ethdb.Batch
	commitFn  func() error
	commitCap int

	// restore
	progress *ProgressMarker

	// network access
	client Client

	// stats
	stats stats.Stats

	// synchronization
	workCh     chan leafSyncJob
	numThreads int
	doneCh     chan struct{}
	err        error

	// informational counters
	start                time.Time
	totalLeafCount       uint64
	totalCodeLen         uint64
	codeCount            uint32
	storageCount         uint32
	existingStorageCount uint32

	// trie db
	trieDB *trie.Database
}

// TrieWriter only has the TryUpdate method necessary to write to a trie.
type TrieWriter interface {
	TryUpdate(key, value []byte) error
}

func NewLeafSyncer(
	nodeType message.NodeType, keyLen int,
	db ethdb.Database, commitFn func() error, commitCap int, progressMarker *ProgressMarker,
	client Client, numThreads int, stats stats.Stats,
) *leafSyncer {
	return &leafSyncer{
		nodeType:  nodeType,
		keyLen:    keyLen,
		db:        db,
		commitFn:  commitFn,
		commitCap: commitCap,
		client:    client,

		numThreads: numThreads,
		workCh:     make(chan leafSyncJob, numThreads),
		doneCh:     make(chan struct{}),
		progress:   progressMarker,

		stats:  stats,
		trieDB: trie.NewDatabaseWithConfig(db, &trie.Config{Cache: 32}),
	}
}

func (l *leafSyncer) NewBatch() ethdb.Batch {
	return ethdb.NewCappedBatch(l.db.NewBatch(), l.commitCap, l.commitFn)
}

func (l *leafSyncer) Start(ctx context.Context, root common.Hash) {
	l.start = time.Now()
	l.root = root
	l.batch = l.NewBatch()

	subctx, cancel := context.WithCancel(ctx)
	eg, egCtx := errgroup.WithContext(subctx)
	for i := 0; i < l.numThreads; i++ {
		eg.Go(func() error {
			for {
				select {
				case work, more := <-l.workCh:
					if !more {
						return nil
					}

					if err := l.syncLeafs(egCtx, work); err != nil {
						cancel()
						return err
					}
				case <-egCtx.Done():
					return egCtx.Err()
				}
			}
		})
	}
	go func() {
		l.err = eg.Wait()
		cancel()
		// only clear progress marker if goroutines completed successfully
		if l.progress != nil && l.err == nil {
			if err := l.progress.Done(l.batch); err != nil {
				l.err = err
			}
		}
		// batch write is done last so the presence of the
		// root node can be considered as completion of the
		// trie sync
		if err := l.batch.Write(); err != nil && l.err == nil {
			l.err = err
		}
		close(l.doneCh)
	}()

	if l.progress != nil {
		accounts, roots := l.progress.Get()
		if len(accounts) > 0 {
			if accounts[0] != (common.Hash{}) {
				log.Error("bad progress marker, expected first entry to be main trie", "account", accounts[0], "root", roots[0])
				l.err = fmt.Errorf("bad progress marker, expected first entry to be main trie, account=%s, root=%s", accounts[0], roots[0])
				close(l.workCh)
				return
			} else if roots[0] != root {
				log.Info("resetting progress marker from previous root", "root", root, "previousRoot", roots[0])
				// progress marker should be reset before starting the new root
				l.progress.Reset()
			} else {
				// leafSyncer uses [numThreads] concurrent goroutines, one for the
				// main trie and [numThreads - 1] for storage tries. The main trie
				// download blocks if all [numThread - 1] storage tries are busy
				// until one becomes available.
				// To maintain this invariant when resuming, we kick off the storage
				// tries previously in progress before resuming syncing the main trie.
				// Otherwise, there's a race condition where tries further ahead
				// (with lexographically greater account hashes) can be started
				// before the ones that were previously in progress.
				// Additionally, this ensures correctness if we are resuming with
				// fewer [numThreads] than the previous sync.
				for i := 1; i < len(accounts); i++ {
					log.Info("recovered progress marker", "account", accounts[i], "root", roots[i])
					l.workCh <- leafSyncJob{root: roots[i], account: accounts[i], batch: l.NewBatch()}
				}
			}
		}
	}
	// always start the root
	l.workCh <- leafSyncJob{root: root, batch: l.batch}
}

// leafSyncJob represents one merkle patricia trie to sync.
// If this is a storage trie, [account] will be set in addition to [root].
// syncLeafs uses this struct to sync leafs and will write them to [batch].
type leafSyncJob struct {
	root    common.Hash
	account common.Hash
	batch   ethdb.Batch
}

// populateStorageSnapshotFromTrie iterates the storage trie for [account]
// at [root] and populates the snapshot using [batch] as the writer.
// To sync a storage trie, this method is called first.
// If we find the trie on disk and successfully iterate it (no errors),
// syncing will be skipped. This occurs if the trie was synced previously
// or if the storage trie of [account] remains current with the state we are
// syncing to.
// The [account] storage snapshot is updated with leafs from the trie during
// iteration.
// Returns an error if the trie is not found on disk or if it cannot be
// completely iterated. In these cases, this trie will be synced from peers.
func (l *leafSyncer) populateStorageSnapshotFromTrie(account common.Hash, root common.Hash, batch ethdb.KeyValueWriter) error {
	start := time.Now()
	tr, err := trie.NewSecure(root, l.trieDB)
	if err != nil {
		return err
	}
	it := trie.NewIterator(tr.NodeIterator(nil))
	leafs := 0
	for it.Next() {
		rawdb.WriteStorageSnapshot(batch, account, common.BytesToHash(it.Key), it.Value[:])
		leafs++
	}
	if err := it.Err; err != nil {
		return err
	}

	existingStorageTries := atomic.AddUint32(&l.existingStorageCount, 1)
	log.Info(
		"restored existing trie to snapshot",
		"root", root, "account", account, "leafs", leafs,
		"time", time.Since(start), "totalTries", existingStorageTries,
	)
	return nil
}

// syncLeafs fetches leafs for account [work.account] (or the main trie if [work.account] is empty)
// at [work.root] using [work.batch] as a writer. This method updates [l.progress] as needed.
func (l *leafSyncer) syncLeafs(ctx context.Context, work leafSyncJob) error {
	if l.progress != nil {
		if err := l.progress.Add(l.db, work.account, work.root); err != nil {
			return err
		}
	}
	trie := trie.NewStackTrie(work.batch)

	// restore data from disk to stack trie
	var start []byte
	if l.nodeType == message.StateTrieNode {
		var err error
		// check if root is already there
		exists, err := l.db.Has(work.root[:])
		if err != nil {
			return err
		}
		if exists && work.account != (common.Hash{}) {
			// populate snapshot with existing data to avoid requesting this from peers
			// this may error during the iteration as pruning may delete the storage trie nodes
			// without deleting the root in which case we fall back to downloading the trie leafs
			// from the network
			if err = l.populateStorageSnapshotFromTrie(work.account, work.root, work.batch); err == nil {
				if err = work.batch.Write(); err != nil {
					return err
				}
				if l.progress != nil {
					l.progress.Remove(work.account)
				}
				return nil
			} else {
				log.Debug("failed to iterate storage snapshot on disk, requesting from network", "account", work.account, "storageRoot", work.root)
			}
		}

		if work.account == (common.Hash{}) {
			it := rawdb.IterateAccountSnapshots(l.db)
			prefixLen := len(rawdb.SnapshotAccountPrefix)
			start, err = l.restoreTrie(trie, prefixLen, it, snapshot.FullAccountRLP, work.root)
		} else {
			it := rawdb.IterateStorageSnapshots(l.db, work.account)
			prefixLen := len(rawdb.SnapshotStoragePrefix) + common.HashLength
			idFunc := func(val []byte) ([]byte, error) { return val, nil } // storage snapshot values stored the same as in the trie
			start, err = l.restoreTrie(trie, prefixLen, it, idFunc, work.root)
		}
		if err != nil {
			return fmt.Errorf("error restoring trie: %w", err)
		}
		if len(start) > 0 {
			incrOne(start)
		}
	}

	totalSize := int64(0)
	onLeaf := func(ctx context.Context, key, value []byte) error {
		leafSize := int64(len(key) + len(value))
		l.stats.IncLeavesReceived(leafSize)
		totalSize += leafSize

		if err := trie.TryUpdate(key, value); err != nil {
			return err
		}

		if l.nodeType == message.StateTrieNode {
			if work.account == (common.Hash{}) {
				return l.onAccount(ctx, key, value)
			}
			rawdb.WriteStorageSnapshot(work.batch, work.account, common.BytesToHash(key), value)
			atomic.AddUint32(&l.storageCount, 1)
			l.stats.UpdateStorageCommitted(int64(len(key) + len(value)))
		}
		return nil
	}
	leafs, err := l.doLeafRequest(ctx, work.root, start, onLeaf)
	if err != nil {
		return err
	}
	atomic.AddUint64(&l.totalLeafCount, 1)
	log.Info(
		"total leafs processed",
		"root", work.root,
		"account", work.account,
		"leafs", leafs,
		"totalTime", time.Since(l.start),
		"storageCount", atomic.LoadUint32(&l.storageCount),
		"codeCount", atomic.LoadUint32(&l.codeCount),
		"totalCodeLen", common.StorageSize(atomic.LoadUint64(&l.totalCodeLen)),
	)
	root, err := trie.Commit()
	if err != nil {
		return err
	}
	l.stats.UpdateTrieCommitted(totalSize)
	if work.root != root { // need to check this to ensure no leafs are missing from the end of the trie
		return fmt.Errorf("root mismatch expected %v got %v", work.root, root)
	}

	// don't write the main trie's batch to disk here, instead it will occur
	// after all parallel getters are completed.
	// this allows detecting if the work is done by checking the presence
	// of the root node on disk.
	if work.root != l.root {
		if err := work.batch.Write(); err != nil {
			return err
		}
		if l.progress != nil {
			l.progress.Remove(work.account)
		}
	} else {
		close(l.workCh)
	}
	return nil
}

// restoreTrie takes an iterator [it] and adds all items returned with key lentgh
// matching [prefixLen+l.keyLen] to the trie [t].
// returns the last key restored and the iterator error if any.
func (l *leafSyncer) restoreTrie(
	t TrieWriter,
	prefixLen int,
	it ethdb.Iterator,
	snapshotToTrie func([]byte) ([]byte, error),
	root common.Hash,
) ([]byte, error) {
	count := uint64(0)
	var lastKey []byte
	defer it.Release()

	for it.Next() {
		// TODO: improve prefix handling
		if len(it.Key()) < prefixLen {
			continue
		}
		key := it.Key()[prefixLen:]
		if len(key) != l.keyLen {
			continue
		}
		lastKey = key
		trieVal, err := snapshotToTrie(it.Value())
		if err != nil {
			return nil, err
		}
		if err := t.TryUpdate(key, trieVal); err != nil {
			return nil, err
		}
		count++
		if count%10000 == 0 {
			log.Info("restored db data to trie (progress)", "count", count, "root", root, "mainTrie", l.root == root)
		}
	}
	if count > 0 {
		log.Info("restored db data to trie", "count", count, "root", root, "mainTrie", l.root == root, "lastKey", common.BytesToHash(lastKey))
	}
	return lastKey, it.Error()
}

// onAccount adds a request to the work queue to fetch the storage trie
// for this account (if any). Additionally, the code (if any) will be fetched
// if not present on disk.
func (l *leafSyncer) onAccount(ctx context.Context, key, value []byte) error {
	var acc types.StateAccount
	if err := rlp.DecodeBytes(value, &acc); err != nil {
		return err
	}
	if acc.Root != (common.Hash{}) && acc.Root != types.EmptyRootHash {
		// here we add a request to the work queue to fetch the storage trie for this account
		// make sure we don't hang on the publish if no more goroutines are listening on
		// l.workChan because the context got cancelled
		select {
		case l.workCh <- leafSyncJob{
			root:    acc.Root,
			account: common.BytesToHash(key),
			batch:   l.NewBatch(),
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	codeHash := common.BytesToHash(acc.CodeHash)
	if codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash {
		if blob := rawdb.ReadCodeWithPrefix(l.db, codeHash); len(blob) == 0 {
			code, err := l.client.GetCode(codeHash) // blocks to receive the code
			if err != nil {
				return err
			}
			rawdb.WriteCode(l.batch, codeHash, code)
			l.stats.UpdateCodeCommitted(int64(common.HashLength + len(code)))
			atomic.AddUint32(&l.codeCount, 1)
			atomic.AddUint64(&l.totalCodeLen, uint64(len(code)))
		}
	}
	// Snapshot supports SlimAccountRLP. This uses less storage by
	// avoiding writing CodeHash & Root if they are known empty values.
	slimAccount := snapshot.SlimAccountRLP(acc.Nonce, acc.Balance, acc.Root, acc.CodeHash, acc.IsMultiCoin)
	rawdb.WriteAccountSnapshot(l.batch, common.BytesToHash(key), slimAccount)
	return nil
}

// Error returns an error if one occurred during sync.
func (l *leafSyncer) Error() error { return l.err }

// Done returns a channel that can be waited on until the sync is complete.
func (l *leafSyncer) Done() chan struct{} { return l.doneCh }

// doLeafRequest gets leafs starting at [start] for the trie at [root] and verifies the range proofs received.
// the function will paginate these requests if a full response is not received. The callback [onLeaf] is called
// for each received leaf.
// Returnes the number of leafs received and an error if one occurred.
func (l *leafSyncer) doLeafRequest(ctx context.Context, root common.Hash, start []byte, onLeaf func(ctx context.Context, key []byte, value []byte) error) (uint, error) {
	totalLeafs := uint(0)

	// special case, allow start to be nil
	if start != nil && len(start) != l.keyLen {
		return 0, fmt.Errorf("keyLen (%d) mismatch with len(start) (%d)", l.keyLen, len(start))
	}
	end := bytes.Repeat([]byte{0xff}, l.keyLen)

	for {
		select {
		// check ctx.Done() and abort early in case the context was cancelled.
		case <-ctx.Done():
			return totalLeafs, ctx.Err()
		default:
		}

		req := message.LeafsRequest{
			Root:     root,
			Start:    start,
			End:      end,
			Limit:    256,
			NodeType: l.nodeType,
		}
		log.Debug("sending leaf request", "request", req, "mainTrie", req.Root == l.root)

		l.stats.IncLeavesRequested()

		startTime := time.Now()
		leafs, err := l.client.GetLeafs(req)
		l.stats.UpdateLeafRequestLatency(time.Since(startTime))
		if err != nil {
			return 0, fmt.Errorf("error receiving leaves (root %v) (start %v) (end %v): %w", root, common.Bytes2Hex(start), common.Bytes2Hex(end), err)
		}
		for i, key := range leafs.Keys {
			if err := onLeaf(ctx, key, leafs.Vals[i]); err != nil {
				return 0, err
			}
		}

		totalLeafs += uint(len(leafs.Keys))
		log.Debug("received leafs", "leafs", len(leafs.Keys), "totalLeafs", totalLeafs, "root", root)
		if !leafs.More {
			break
		}

		start = common.CopyBytes(leafs.Keys[len(leafs.Keys)-1])
		incrOne(start)
	}
	return totalLeafs, nil
}

// increment byte array value by one
func incrOne(bytes []byte) {
	index := len(bytes) - 1
	for index >= 0 {
		if bytes[index] < 255 {
			bytes[index]++
			break
		} else {
			bytes[index] = 0
			index--
		}
	}
}
