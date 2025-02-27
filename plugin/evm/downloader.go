// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"sync"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

const (
	// Dynamic state switches state root occasionally
	// Buffer must be large enough to
	pivotInterval = 128
	bufferSize    = 3 * pivotInterval
)

type queueElement struct {
	block *Block
	req   syncBlockRequest
}

type downloader struct {
	pivotBlock  *Block
	snapSyncer  *snap.Syncer
	blockBuffer []*queueElement
	bufferLen   int
	bufferLock  sync.Mutex

	stateSyncStart chan *stateSync
	newPivot       chan *Block
	quitCh         chan struct{} // Quit channel to signal termination
	// quitLock       sync.Mutex    // Lock to prevent double closes
}

func newDownloader(chaindb ethdb.Database, firstPivot *Block) *downloader {
	d := &downloader{
		pivotBlock:     firstPivot,
		snapSyncer:     snap.NewSyncer(chaindb, rawdb.HashScheme),
		blockBuffer:    make([]*queueElement, bufferSize),
		stateSyncStart: make(chan *stateSync),
		quitCh:         make(chan struct{}),
		newPivot:       make(chan *Block),
	}

	go d.stateFetcher()

	return d
}

// stateFetcher manages the active state sync and accepts requests
// on its behalf.
func (d *downloader) stateFetcher() {
	for {
		select {
		case s := <-d.stateSyncStart:
			for next := s; next != nil; {
				next = d.runStateSync(next)
			}
		case <-d.quitCh:
			return
		}
	}
}

// QueueBlock queues a block for processing by the state syncer.
// This assumes the queue lock is NOT held
func (d *downloader) QueueBlockOrPivot(b *Block, req syncBlockRequest) error {
	d.bufferLock.Lock()
	defer d.bufferLock.Unlock()
	if d.bufferLen >= len(d.blockBuffer) {
		return errors.New("Snap sync queue overflow")
	}

	d.blockBuffer[d.bufferLen] = &queueElement{b, req}
	d.bufferLen++

	log.Debug("Received queue request", "hash", b.ID(), "height", b.Height(), "req", req, "timestamp", b.Timestamp())

	if req == acceptSyncBlockRequest && b.Height() >= d.pivotBlock.Height()+pivotInterval {
		log.Info("Setting new pivot block", "hash", b.ID(), "height", b.Height(), "timestamp", b.Timestamp())

		// Reset pivot first in other goroutine
		d.pivotBlock = b
		d.newPivot <- b

		// Clear queue
		if err := d.flushQueue(false); err != nil {
			close(d.quitCh)
			return err
		}
	} else if b.Height() <= d.pivotBlock.Height() {
		close(d.quitCh)
		log.Warn("Received block with height less than pivot block", "hash", b.ID(), "height", b.Height(), "timestamp", b.Timestamp())
		return errors.New("received block with height less than pivot block")
	}
	return nil
}

// Clears queue of blocks. Assumes no elements are past pivot and bufferLock is held
// If `final`, executes blocks as normal. Otherwise executes only atomic operations
// To avoid duplicating actions, should adjust length at higher level
func (d *downloader) flushQueue(final bool) error {
	defer func() { d.bufferLen = 0 }()
	for i, elem := range d.blockBuffer {
		if i >= d.bufferLen {
			return nil
		}

		if err := elem.block.ExecuteSyncRequest(elem.req, final); err != nil {
			if final {
				// EXTREMELY hacky solution to release lock in final case on error - should fix later
				d.bufferLock.Unlock()
			}
			return err
		}
	}

	return nil
}

// processSnapSyncContent takes fetch results from the queue and writes them to the
// database. It also controls the synchronisation of state nodes of the pivot block.
func (d *downloader) SnapSync() error {
	// Start syncing state of the reported head block. This should get us most of
	// the state of the pivot block.
	sync := d.syncState(d.pivotBlock.ethBlock.Root())

	defer func() {
		// The `sync` object is replaced every time the pivot moves. We need to
		// defer close the very last active one, hence the lazy evaluation vs.
		// calling defer sync.Cancel() !!!
		sync.Cancel()
	}()

	for {
		select {
		// If stateSync is ended, clear queue and return
		// If err, just return so we can see it
		case <-sync.done:
			if sync.err != nil {
				return sync.err
			}
			d.bufferLock.Lock() // unlocked by syncer client once we can normally process blocks
			return d.flushQueue(true)
		case newPivot := <-d.newPivot:
			// If a new pivot block is found, cancel the current state sync and
			// start a new one.
			sync.Cancel()
			sync = d.syncState(newPivot.ethBlock.Root())
		}
	}
}

// syncState starts downloading state with the given root hash.
func (d *downloader) syncState(root common.Hash) *stateSync {
	// Create the state sync
	s := newStateSync(d, root)
	select {
	case d.stateSyncStart <- s:
		// If we tell the statesync to restart with a new root, we also need
		// to wait for it to actually also start -- when old requests have timed
		// out or been delivered
		<-s.started
	case <-d.quitCh:
		s.err = errors.New("errCancelStateFetch") //errCancelStateFetch from geth
		close(s.done)
	}
	return s
}

// runStateSync runs a state synchronisation until it completes or another root
// hash is requested to be switched over to.
func (d *downloader) runStateSync(s *stateSync) *stateSync {
	log.Debug("State sync starting", "root", s.root)

	go s.run()
	defer s.Cancel()

	for {
		select {
		case next := <-d.stateSyncStart:
			return next

		case <-s.done:
			return nil
		}
	}
}

// stateSync schedules requests for downloading a particular state trie defined
// by a given state root.
type stateSync struct {
	d    *downloader // Downloader instance to access and manage current peerset
	root common.Hash // State root currently being synced

	started    chan struct{} // Started is signalled once the sync loop starts
	cancel     chan struct{} // Channel to signal a termination request
	cancelOnce sync.Once     // Ensures cancel only ever gets called once
	done       chan struct{} // Channel to signal termination completion
	err        error         // Any error hit during sync (set before completion)
}

// newStateSync creates a new state trie download scheduler. This method does not
// yet start the sync. The user needs to call run to initiate.
func newStateSync(d *downloader, root common.Hash) *stateSync {
	return &stateSync{
		d:       d,
		root:    root,
		cancel:  make(chan struct{}),
		done:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

// run starts the task assignment and response processing loop, blocking until
// it finishes, and finally notifying any goroutines waiting for the loop to
// finish.
func (s *stateSync) run() {
	close(s.started)
	s.err = s.d.snapSyncer.Sync(s.root, s.cancel)
	close(s.done)
}

// Wait blocks until the sync is done or canceled.
func (s *stateSync) Wait() error {
	<-s.done
	return s.err
}

// Cancel cancels the sync and waits until it has shut down.
func (s *stateSync) Cancel() error {
	s.cancelOnce.Do(func() {
		close(s.cancel)
	})
	return s.Wait()
}
