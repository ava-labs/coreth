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

	"github.com/ava-labs/coreth/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	targetTasksPerWorker = 8
	maxConcurrentReads   = 32
	subfetcherMaxCopies  = 16
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
	fetches  map[string]Trie        // Partially or fully fetcher tries
	fetchers map[string]*subfetcher // Subfetchers for each trie

	workersWg   sync.WaitGroup
	taskQueue   chan func()
	stopWorkers chan struct{} // Interrupts workers
	workersTerm chan struct{} // Closed when all workers terminate

	deliveryCopyMissMeter    metrics.Meter
	deliveryRequestMissMeter metrics.Meter
	deliveryWaitMissMeter    metrics.Meter

	accountLoadMeter  metrics.Meter
	accountDupMeter   metrics.Meter
	accountWasteMeter metrics.Meter
	storageLoadMeter  metrics.Meter
	storageDupMeter   metrics.Meter
	storageWasteMeter metrics.Meter
}

func newTriePrefetcher(db Database, root common.Hash, namespace string) *triePrefetcher {
	prefix := triePrefetchMetricsPrefix + namespace
	p := &triePrefetcher{
		db:       db,
		root:     root,
		fetchers: make(map[string]*subfetcher), // Active prefetchers use the fetchers map

		taskQueue:   make(chan func()),
		stopWorkers: make(chan struct{}),
		workersTerm: make(chan struct{}),

		deliveryCopyMissMeter:    metrics.GetOrRegisterMeter(prefix+"/deliverymiss/copy", nil),
		deliveryRequestMissMeter: metrics.GetOrRegisterMeter(prefix+"/deliverymiss/request", nil),
		deliveryWaitMissMeter:    metrics.GetOrRegisterMeter(prefix+"/deliverymiss/wait", nil),

		accountLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/account/load", nil),
		accountDupMeter:   metrics.GetOrRegisterMeter(prefix+"/account/dup", nil),
		accountWasteMeter: metrics.GetOrRegisterMeter(prefix+"/account/waste", nil),
		storageLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/storage/load", nil),
		storageDupMeter:   metrics.GetOrRegisterMeter(prefix+"/storage/dup", nil),
		storageWasteMeter: metrics.GetOrRegisterMeter(prefix+"/storage/waste", nil),
	}

	// Use workers to limit max concurrent readers
	for i := 0; i < maxConcurrentReads; i++ {
		p.workersWg.Add(1)
		go func() {
			defer p.workersWg.Done()

			for {
				select {
				case task := <-p.taskQueue:
					task()
				case <-p.stopWorkers:
					return
				}
			}
		}()
	}
	go func() {
		p.workersWg.Wait()
		close(p.workersTerm)
	}()
	return p
}

// close iterates over all the subfetchers, aborts any that were left spinning
// and reports the stats to the metrics subsystem.
func (p *triePrefetcher) close() {
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}

	// Collect stats from all fetchers
	for _, fetcher := range p.fetchers {
		fetcher.abort() // safe to call multiple times

		if metrics.Enabled {
			if fetcher.root == p.root {
				p.accountLoadMeter.Mark(int64(len(fetcher.seen)))
				p.accountDupMeter.Mark(int64(fetcher.dups))

				for _, key := range fetcher.used {
					delete(fetcher.seen, string(key))
				}
				p.accountWasteMeter.Mark(int64(len(fetcher.seen)))
			} else {
				p.storageLoadMeter.Mark(int64(len(fetcher.seen)))
				p.storageDupMeter.Mark(int64(fetcher.dups))

				for _, key := range fetcher.used {
					delete(fetcher.seen, string(key))
				}
				p.storageWasteMeter.Mark(int64(len(fetcher.seen)))
			}
		}
	}

	// Stop all workers once fetchers are aborted (otherwise
	// could stop while waiting)
	close(p.stopWorkers)
	<-p.workersTerm

	// Clear out all fetchers (will crash on a second call, deliberate)
	p.fetchers = nil
}

// copy creates a deep-but-inactive copy of the trie prefetcher. Any trie data
// already loaded will be copied over, but no goroutines will be started. This
// is mostly used in the miner which creates a copy of it's actively mutated
// state to be sealed while it may further mutate the state.
func (p *triePrefetcher) copy() *triePrefetcher {
	copy := &triePrefetcher{
		db:      p.db,
		root:    p.root,
		fetches: make(map[string]Trie), // Active prefetchers use the fetchers map

		deliveryCopyMissMeter:    p.deliveryCopyMissMeter,
		deliveryRequestMissMeter: p.deliveryRequestMissMeter,
		deliveryWaitMissMeter:    p.deliveryWaitMissMeter,

		accountLoadMeter:  p.accountLoadMeter,
		accountDupMeter:   p.accountDupMeter,
		accountWasteMeter: p.accountWasteMeter,
		storageLoadMeter:  p.storageLoadMeter,
		storageDupMeter:   p.storageDupMeter,
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
		copy.fetches[id] = fetcher.peek()
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

// trie returns the trie matching the root hash, or nil if the prefetcher doesn't
// have it.
func (p *triePrefetcher) trie(owner common.Hash, root common.Hash) Trie {
	// If the prefetcher is inactive, return from existing deep copies
	id := p.trieID(owner, root)
	if p.fetches != nil {
		trie := p.fetches[id]
		if trie == nil {
			p.deliveryCopyMissMeter.Mark(1)
			return nil
		}
		return p.db.CopyTrie(trie)
	}

	// Otherwise the prefetcher is active, bail if no trie was prefetched for this root
	fetcher := p.fetchers[id]
	if fetcher == nil {
		p.deliveryRequestMissMeter.Mark(1)
		return nil
	}

	// Wait for the fetcher to finish, if it exists (this will prevent any future tasks from
	// being enqueued)
	fetcher.wait()

	// Shutdown any remaining fetcher goroutines to free memory as soon as possible
	fetcher.abort()

	// Return a copy of one of the prefetched tries
	trie := fetcher.peek()
	if trie == nil {
		p.deliveryWaitMissMeter.Mark(1)
		return nil
	}
	return trie
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
	return string(append(owner.Bytes(), root.Bytes()...))
}

// subfetcher is a trie fetcher goroutine responsible for pulling entries for a
// single trie. It is spawned when a new root is encountered and lives until the
// main prefetcher is paused and either all requested items are processed or if
// the trie being worked on is retrieved from the prefetcher.
type subfetcher struct {
	p *triePrefetcher

	db    Database       // Database to load trie nodes through
	state common.Hash    // Root hash of the state to prefetch
	owner common.Hash    // Owner of the trie, usually account hash
	root  common.Hash    // Root hash of the trie to prefetch
	addr  common.Address // Address of the account that the trie belongs to

	to *trieOrchestrator // Orchestrate concurrent fetching of a single trie

	seen map[string]struct{} // Tracks the entries already loaded
	dups int                 // Number of duplicate preload tasks
	used [][]byte            // Tracks the entries used in the end
}

// newSubfetcher creates a goroutine to prefetch state items belonging to a
// particular root hash.
func newSubfetcher(p *triePrefetcher, owner common.Hash, root common.Hash, addr common.Address) *subfetcher {
	sf := &subfetcher{
		p:     p,
		db:    p.db,
		state: p.root,
		owner: owner,
		root:  root,
		addr:  addr,
		seen:  make(map[string]struct{}),
	}
	sf.to = newTrieOrchestrator(sf)
	if sf.to != nil {
		go sf.to.processTasks()
	}
	return sf
}

// schedule adds a batch of trie keys to the queue to prefetch.
// This should never block, so an array is used instead of a channel.
func (sf *subfetcher) schedule(keys [][]byte) {
	// Append the tasks to the current queue
	tasks := make([][]byte, 0, len(keys))
	for _, key := range keys {
		// Check if keys already seen
		sk := string(key)
		if _, ok := sf.seen[sk]; ok {
			sf.dups++
			continue
		}
		sf.seen[sk] = struct{}{}
		tasks = append(tasks, key)
	}

	// After counting keys, exit if they can't be prefetched
	if sf.to == nil {
		return
	}

	// Add tasks to queue for prefetching
	sf.to.enqueueTasks(tasks)
}

// peek tries to retrieve a deep copy of the fetcher's trie in whatever form it
// is currently.
func (sf *subfetcher) peek() Trie {
	if sf.to == nil {
		return nil
	}
	return sf.to.copyBase()
}

// wait must only be called if [triePrefetcher] has not been closed. If this happens,
// workers will not finish.
func (sf *subfetcher) wait() {
	if sf.to == nil {
		// Unable to open trie
		return
	}
	sf.to.wait()
}

func (sf *subfetcher) abort() {
	if sf.to == nil {
		// Unable to open trie
		return
	}
	sf.to.abort()
}

// lockableTrie is used to ensure that a trie is not accessed concurrently
// (could only occur when copying)
type lockableTrie struct {
	t Trie
	l sync.Mutex
}

// trieOrchestrator is not thread-safe.
type trieOrchestrator struct {
	sf *subfetcher

	base *lockableTrie

	outstandingRequests sync.WaitGroup

	tasksAllowed     bool
	pendingTasks     [][]byte
	pendingTasksLock sync.Mutex
	wake             chan struct{}
	loopTerm         chan struct{}

	copies   int
	copyChan chan *lockableTrie

	stop     chan struct{}
	stopOnce sync.Once
}

func newTrieOrchestrator(sf *subfetcher) *trieOrchestrator {
	// Start by opening the trie and stop processing if it fails
	var (
		base Trie
		err  error
	)
	if sf.owner == (common.Hash{}) {
		base, err = sf.db.OpenTrie(sf.root)
		if err != nil {
			log.Warn("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
			return nil
		}
	} else {
		base, err = sf.db.OpenStorageTrie(sf.state, sf.owner, sf.root)
		if err != nil {
			log.Warn("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
			return nil
		}
	}

	// Create initial trie copy
	ct := &lockableTrie{t: base}
	to := &trieOrchestrator{
		sf:   sf,
		base: ct,

		tasksAllowed: true,
		wake:         make(chan struct{}, 1),
		loopTerm:     make(chan struct{}),

		copies:   1,
		copyChan: make(chan *lockableTrie, subfetcherMaxCopies),

		stop: make(chan struct{}),
	}
	to.copyChan <- ct
	return to
}

func (to *trieOrchestrator) copyBase() Trie {
	to.base.l.Lock()
	defer to.base.l.Unlock()

	return to.sf.db.CopyTrie(to.base.t)
}

func (to *trieOrchestrator) enqueueTasks(tasks [][]byte) {
	if len(tasks) == 0 {
		return
	}

	// Add tasks to [pendingTasks]
	to.pendingTasksLock.Lock()
	if !to.tasksAllowed {
		to.pendingTasksLock.Unlock()
		return
	}
	to.pendingTasks = append(to.pendingTasks, tasks...)
	to.pendingTasksLock.Unlock()

	// Wake up processor
	select {
	case to.wake <- struct{}{}:
	default:
	}
}

func (to *trieOrchestrator) processTasks() {
	defer close(to.loopTerm)

	for {
		// Determine if we should process or exit
		select {
		case <-to.wake:
		case <-to.stop:
			return
		}

		// Get current tasks
		to.pendingTasksLock.Lock()
		tasks := to.pendingTasks
		to.pendingTasks = nil
		to.pendingTasksLock.Unlock()

		// Create more copies if we have a lot of work
		lt := len(tasks)
		tasksPerWorker := lt / to.copies
		if tasksPerWorker > targetTasksPerWorker && to.copies < subfetcherMaxCopies {
			extraPerWorker := (tasksPerWorker - targetTasksPerWorker) * to.copies
			newWorkers := extraPerWorker / targetTasksPerWorker
			for i := 0; i < newWorkers && to.copies+1 <= subfetcherMaxCopies; i++ {
				to.copies++
				to.copyChan <- &lockableTrie{t: to.copyBase()}
			}
		}
		workSize := lt / to.copies

		// Enqueue more work as soon as trie copies are available
		for i := 0; i < lt; i += workSize {
			end := i + workSize
			if end > lt {
				end = lt
			}
			thisTask := tasks[i:end]

			// Wait for an available copy
			var t *lockableTrie
			select {
			case t = <-to.copyChan:
			case <-to.stop:
				return
			}

			to.outstandingRequests.Add(lt)
			f := func() {
				for _, task := range thisTask {
					// Check if we should stop
					var stopping bool
					select {
					case <-to.stop:
						// Ensure we don't forget to mark tasks we've been
						// given as complete.
						to.outstandingRequests.Done()
						stopping = true
					default:
					}
					if stopping {
						continue
					}

					// Perform task
					t.l.Lock()
					var err error
					if len(task) == common.AddressLength {
						_, err = t.t.GetAccount(common.BytesToAddress(task))
					} else {
						_, err = t.t.GetStorage(to.sf.addr, task)
					}
					t.l.Unlock()
					if err != nil {
						log.Error("Trie prefetcher failed fetching", "root", to.sf.root, "err", err)
					}
					to.outstandingRequests.Done()
				}

				// Return copy when we are done with it, so someone else can use it
				//
				// channel should be buffered and should not block
				to.copyChan <- t
			}

			// Enqueue work, unless stopped.
			select {
			case to.sf.p.taskQueue <- f:
			case <-to.stop:
				// Return all counters
				for i := 0; i < lt; i++ {
					to.outstandingRequests.Done()
				}
				return
			}
		}
	}
}

func (to *trieOrchestrator) wait() {
	// Prevent more tasks from being enqueued
	to.pendingTasksLock.Lock()
	to.tasksAllowed = false
	to.pendingTasksLock.Unlock()

	// Wait for ongoing tasks to complete
	to.outstandingRequests.Wait()
}

// abort stops any ongoing tasks
func (to *trieOrchestrator) abort() {
	// Prevent more tasks from being enqueued
	to.pendingTasksLock.Lock()
	to.tasksAllowed = false
	to.pendingTasksLock.Unlock()

	// Stop all ongoing tasks
	to.stopOnce.Do(func() {
		close(to.stop)
	})
	<-to.loopTerm
	to.outstandingRequests.Wait()

	// Clear copies
	to.copyChan = nil
}
