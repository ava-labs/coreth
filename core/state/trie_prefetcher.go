// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

var (
	// triePrefetchMetricsPrefix is the prefix under which to publish the metrics.
	triePrefetchMetricsPrefix = "trie/prefetch/"
)

// triePrefetcher is an active prefetcher, which receives accounts or storage
// items and does trie-loading of them. The goal is to get as much useful content
// into the caches as possible.
//
// Note, the prefetcher's API is not thread safe.
type triePrefetcher struct {
	db       Database               // Database to fetch trie nodes through
	root     common.Hash            // Root hash of the account trie for metrics
	fetches  map[string]Trie        // Partially or fully fetched tries. Only populated for inactive copies.
	fetchers map[string]*subfetcher // Subfetchers for each trie

	bw             *BoundedWorkers
	maxParallelism int

	accountLoadMeter  metrics.Meter
	accountDupMeter   metrics.Meter
	accountSkipMeter  metrics.Meter
	accountWasteMeter metrics.Meter
	storageLoadMeter  metrics.Meter
	storageDupMeter   metrics.Meter
	storageSkipMeter  metrics.Meter
	storageWasteMeter metrics.Meter
}

func newTriePrefetcher(db Database, root common.Hash, namespace string, parallelism int) *triePrefetcher {
	if parallelism <= 0 {
		parallelism = 1
	}
	prefix := triePrefetchMetricsPrefix + namespace
	p := &triePrefetcher{
		db:       db,
		root:     root,
		fetchers: make(map[string]*subfetcher), // Active prefetchers use the fetchers map

		accountLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/account/load", nil),
		accountDupMeter:   metrics.GetOrRegisterMeter(prefix+"/account/dup", nil),
		accountSkipMeter:  metrics.GetOrRegisterMeter(prefix+"/account/skip", nil),
		accountWasteMeter: metrics.GetOrRegisterMeter(prefix+"/account/waste", nil),
		storageLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/storage/load", nil),
		storageDupMeter:   metrics.GetOrRegisterMeter(prefix+"/storage/dup", nil),
		storageSkipMeter:  metrics.GetOrRegisterMeter(prefix+"/storage/skip", nil),
		storageWasteMeter: metrics.GetOrRegisterMeter(prefix+"/storage/waste", nil),
		bw:                NewBoundedWorkers(parallelism),
		maxParallelism:    parallelism,
	}
	return p
}

// close iterates over all the subfetchers, aborts any that were left spinning
// and reports the stats to the metrics subsystem.
func (p *triePrefetcher) close() {
	for _, fetcher := range p.fetchers {
		fetcher.abort() // safe to do multiple times

		if metrics.Enabled {
			if fetcher.root == p.root {
				p.accountLoadMeter.Mark(int64(len(fetcher.seen)))
				p.accountDupMeter.Mark(int64(fetcher.dups))
				p.accountSkipMeter.Mark(int64(len(fetcher.tasks)))

				for _, key := range fetcher.used {
					delete(fetcher.seen, string(key))
				}
				p.accountWasteMeter.Mark(int64(len(fetcher.seen)))
			} else {
				p.storageLoadMeter.Mark(int64(len(fetcher.seen)))
				p.storageDupMeter.Mark(int64(fetcher.dups))
				p.storageSkipMeter.Mark(int64(len(fetcher.tasks)))

				for _, key := range fetcher.used {
					delete(fetcher.seen, string(key))
				}
				p.storageWasteMeter.Mark(int64(len(fetcher.seen)))
			}
		}
	}
	// Clear out all fetchers (will crash on a second call, deliberate)
	p.fetchers = nil
	p.bw.Stop()
}

// copy creates a deep-but-inactive copy of the trie prefetcher. Any trie data
// already loaded will be copied over, but no goroutines will be started. This
// is mostly used in the miner which creates a copy of it's actively mutated
// state to be sealed while it may further mutate the state.
func (p *triePrefetcher) copy() *triePrefetcher {
	copy := &triePrefetcher{
		db:             p.db,
		root:           p.root,
		fetches:        make(map[string]Trie), // Active prefetchers use the fetches map
		maxParallelism: p.maxParallelism,

		accountLoadMeter:  p.accountLoadMeter,
		accountDupMeter:   p.accountDupMeter,
		accountSkipMeter:  p.accountSkipMeter,
		accountWasteMeter: p.accountWasteMeter,
		storageLoadMeter:  p.storageLoadMeter,
		storageDupMeter:   p.storageDupMeter,
		storageSkipMeter:  p.storageSkipMeter,
		storageWasteMeter: p.storageWasteMeter,
	}
	// If the prefetcher is already a copy, duplicate the data
	if p.fetches != nil {
		for root, fetch := range p.fetches {
			if fetch == nil {
				continue
			}
			copy.fetches[root] = p.db.CopyTrie(fetch)
		}
		return copy
	}
	// Otherwise we're copying an active fetcher, retrieve the current states
	for id, fetcher := range p.fetchers {
		copy.fetches[id] = fetcher.db.CopyTrie(fetcher.trie)
	}
	return copy
}

// prefetch schedules a batch of trie items to prefetch.
func (p *triePrefetcher) prefetch(owner common.Hash, root common.Hash, addr common.Address, keys [][]byte) {
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}
	// Active fetcher, schedule the retrievals
	id := p.trieID(owner, root)
	fetcher := p.fetchers[id]
	if fetcher == nil {
		fetcher = newSubfetcher(p, owner, root, addr)
		p.fetchers[id] = fetcher
	}
	fetcher.schedule(keys)
}

// used marks a batch of state items used to allow creating statistics as to
// how useful or wasteful the prefetcher is.
func (p *triePrefetcher) used(owner common.Hash, root common.Hash, used [][]byte) {
	if fetcher := p.fetchers[p.trieID(owner, root)]; fetcher != nil {
		fetcher.used = used
	}
}

// trieID returns an unique trie identifier consists the trie owner and root hash.
func (p *triePrefetcher) trieID(owner common.Hash, root common.Hash) string {
	trieID := make([]byte, common.HashLength*2)
	copy(trieID, owner.Bytes())
	copy(trieID[common.HashLength:], root.Bytes())
	return string(trieID)
}

// subfetcher is a trie fetcher goroutine responsible for pulling entries for a
// single trie. It is spawned when a new root is encountered and lives until the
// main prefetcher is paused and either all requested items are processed or if
// the trie being worked on is retrieved from the prefetcher.
type subfetcher struct {
	db    Database       // Database to load trie nodes through
	state common.Hash    // Root hash of the state to prefetch
	owner common.Hash    // Owner of the trie, usually account hash
	root  common.Hash    // Root hash of the trie to prefetch
	addr  common.Address // Address of the account that the trie belongs to
	trie  Trie           // Trie being populated with nodes

	tasks     [][]byte            // Items queued up for retrieval
	scheduled map[string]struct{} // Map of tasks that have been scheduled to prevent duplicate work
	lock      sync.Mutex          // Lock protecting the task queue

	wake chan struct{} // Wake channel if a new task is scheduled
	stop chan struct{} // Channel to interrupt processing
	term chan struct{} // Channel to signal interruption

	seenLock sync.RWMutex
	seen     map[string]struct{} // Tracks the entries already loaded
	dups     int                 // Number of duplicate preload tasks
	used     [][]byte            // Tracks the entries used in the end

	bw *BoundedWorkers

	trCopies chan Trie
	trSema   chan struct{}
}

// newSubfetcher creates a goroutine to prefetch state items belonging to a
// particular root hash.
func newSubfetcher(p *triePrefetcher, owner common.Hash, root common.Hash, addr common.Address) *subfetcher {
	if p.maxParallelism < 10 {
		panic("what's happening")
	}
	sf := &subfetcher{
		db:        p.db,
		state:     p.root,
		bw:        p.bw,
		owner:     owner,
		root:      root,
		addr:      addr,
		wake:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
		term:      make(chan struct{}),
		seen:      make(map[string]struct{}),
		scheduled: make(map[string]struct{}),
		trCopies:  make(chan Trie, p.maxParallelism),
		trSema:    make(chan struct{}, p.maxParallelism),
	}
	// Start by opening the trie and stop processing (skip calling loop) if it fails
	if sf.owner == (common.Hash{}) {
		trie, err := sf.db.OpenTrie(sf.root)
		if err != nil {
			log.Warn("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
			return sf
		}
		sf.trie = trie
	} else {
		trie, err := sf.db.OpenStorageTrie(sf.state, sf.addr.Hash(), sf.root)
		if err != nil {
			log.Warn("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
			return sf
		}
		sf.trie = trie
	}

	go sf.loop()
	return sf
}

// schedule adds a batch of trie keys to the queue to prefetch.
func (sf *subfetcher) schedule(keys [][]byte) {
	// Append the tasks to the current queue
	sf.lock.Lock()
	for _, key := range keys {
		if _, ok := sf.scheduled[string(key)]; ok {
			continue
		} else {
			sf.tasks = append(sf.tasks, key)
			sf.scheduled[string(key)] = struct{}{}
		}
	}
	sf.lock.Unlock()

	// Notify the prefetcher, it's fine if it's already terminated
	select {
	case sf.wake <- struct{}{}:
	default:
	}
}

// abort interrupts the subfetcher immediately. It is safe to call abort multiple
// times but it is not thread safe.
func (sf *subfetcher) abort() {
	select {
	case <-sf.stop:
	default:
		close(sf.stop)
	}
	<-sf.term
}

// loop waits for new tasks to be scheduled and keeps loading them until it runs
// out of tasks or its underlying trie is retrieved for committing.
func (sf *subfetcher) loop() {
	// No matter how the loop stops, signal anyone waiting that it's terminated
	defer close(sf.term)

	// Trie opened successfully, keep prefetching items
	for {
		select {
		case <-sf.wake:
			// Subfetcher was woken up, retrieve any tasks to avoid spinning the lock
			sf.lock.Lock()
			tasks := sf.tasks
			sf.tasks = nil
			sf.lock.Unlock()

			remainingTasks := sf.fetchTasks(tasks)
			if len(remainingTasks) > 0 {
				sf.lock.Lock()
				sf.tasks = remainingTasks
				sf.lock.Unlock()
			}

		case <-sf.stop:
			// Termination is requested, abort and leave remaining tasks
			return
		}
	}
}

// fetchTasks assumes that it has sole access to sf.trie
func (sf *subfetcher) fetchTasks(tasks [][]byte) [][]byte {
	work := make(chan []byte, len(tasks))
	for _, task := range tasks {
		work <- task
	}
	tasks = tasks[:0] // Empty tasks, but hold onto the memory already allocated to tasks
	close(work)

	wg := sync.WaitGroup{}
	// Range over the work, starting an additional goroutine on demand or performing the work serially
	// using [bw].
	for task := range work {
		t := sf.getTrieCopy()
		task := task
		wg.Add(1)
		sf.bw.Execute(func() {
			defer wg.Done()

			sf.fetchTask(t, task)

			for task := range work {
				sf.fetchTask(t, task)
			}
			sf.trCopies <- t
		})
	}
	wg.Wait()

	// Read remaining tasks from the channel and return unfetched leftovers to the caller
	for task := range work {
		tasks = append(tasks, task)
	}
	return tasks
}

func (sf *subfetcher) getTrieCopy() Trie {
	select {
	case tr := <-sf.trCopies:
		return tr
	case sf.trSema <- struct{}{}:
		return sf.db.CopyTrie(sf.trie)
	}
}

func (sf *subfetcher) fetchTask(t Trie, task []byte) {
	// Mark it as seen before releasing the lock and performing the read
	// to ensure we don't duplicate reads
	sf.seenLock.Lock()
	sf.seen[string(task)] = struct{}{}
	sf.seenLock.Unlock()

	if len(task) == common.AddressLength {
		t.GetAccount(common.BytesToAddress(task))
	} else {
		t.GetStorage(sf.addr, task)
	}
}
