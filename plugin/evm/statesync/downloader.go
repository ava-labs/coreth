// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

type SyncBlockRequest uint8

const (
	// Constants to identify block requests
	VerifySyncBlockRequest SyncBlockRequest = iota + 1
	AcceptSyncBlockRequest
	RejectSyncBlockRequest
)

const (
	// Dynamic state switches state root occasionally
	// Buffer must be large enough to
	pivotInterval = 128
	bufferSize    = 3 * pivotInterval
)

type queueElement struct {
	block    *types.Block
	req      SyncBlockRequest
	resolver func() error
}

type Downloader struct {
	pivotBlock  *types.Block
	pivotLock   sync.RWMutex
	SnapSyncer  *snap.Syncer
	blockBuffer []*queueElement
	bufferLen   int
	bufferLock  *sync.Mutex

	stateSyncStart chan *stateSync
	newPivot       chan *types.Block
	quitCh         chan struct{} // Quit channel to signal termination
	// quitLock       sync.Mutex    // Lock to prevent double closes
}

func NewDownloader(chaindb ethdb.Database, firstPivot *types.Block, bufferLock *sync.Mutex) *Downloader {
	d := &Downloader{
		pivotBlock:     firstPivot,
		SnapSyncer:     snap.NewSyncer(chaindb, rawdb.HashScheme),
		blockBuffer:    make([]*queueElement, bufferSize),
		stateSyncStart: make(chan *stateSync),
		quitCh:         make(chan struct{}),
		newPivot:       make(chan *types.Block),
		bufferLock:     bufferLock,
	}

	go d.stateFetcher()

	return d
}

// stateFetcher manages the active state sync and accepts requests
// on its behalf.
func (d *Downloader) stateFetcher() {
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

// Returns the current pivot
func (d *Downloader) Pivot() *types.Block {
	d.pivotLock.RLock()
	defer d.pivotLock.RUnlock()
	return d.pivotBlock
}

// Opens bufferLock to allow block requests to go through after finalizing the sync
func (d *Downloader) Close() {
	if err := d.flushQueue(true); err != nil {
		log.Error("Issue flushing queue", "err", err)
	}
	d.bufferLock.Unlock()
}

// QueueBlock queues a block for processing by the state syncer.
// This assumes the queue lock is NOT held
func (d *Downloader) QueueBlockOrPivot(b *types.Block, req SyncBlockRequest, resolver func() error) error {
	d.bufferLock.Lock()
	defer d.bufferLock.Unlock()
	if d.bufferLen >= len(d.blockBuffer) {
		close(d.quitCh)
		return errors.New("Snap sync queue overflow")
	}

	d.blockBuffer[d.bufferLen] = &queueElement{b, req, resolver}
	d.bufferLen++

	// Should change to debug prior to production
	log.Info("Received queue request", "hash", b.Hash(), "height", b.Number(), "req type", req)

	// If on pivot interval, we should pivot (regardless of whether the queue is full)
	if req == AcceptSyncBlockRequest && b.NumberU64()%pivotInterval == 0 {
		log.Info("Setting new pivot block", "hash", b.Hash(), "height", b.NumberU64())
		if b.NumberU64() <= d.pivotBlock.NumberU64() {
			log.Warn("Received pivot with height <= pivot block", "old hash", b.Hash(), "old height")
		}

		// Reset pivot first in other goroutine
		d.pivotLock.Lock()
		d.pivotBlock = b
		d.pivotLock.Unlock()
		d.newPivot <- b

		// Clear queue
		if err := d.flushQueue(false); err != nil {
			log.Error("Issue flushing queue", "err", err)
			close(d.quitCh)
			return err
		}
	}

	return nil
}

// Clears queue of blocks. Assumes no elements are past pivot and bufferLock is held
// If `final`, executes blocks as normal. Otherwise executes only atomic operations
// To avoid duplicating actions, should adjust length at higher level
func (d *Downloader) flushQueue(final bool) error {
	defer func() { d.bufferLen = 0 }()
	// During sync, can ignore queue
	log.Debug("Flushing queue", "final", final, "bufferLen", d.bufferLen)
	if !final {
		return nil
	}

	for i, elem := range d.blockBuffer {
		if i >= d.bufferLen {
			return nil
		}

		if err := elem.resolver(); err != nil {
			return err
		}
	}

	return nil
}

// processSnapSyncContent takes fetch results from the queue and writes them to the
// database. It also controls the synchronisation of state nodes of the pivot block.
func (d *Downloader) SnapSync(ctx context.Context) error {
	// Start syncing state of the reported head block. This should get us most of
	// the state of the pivot block.
	sync := d.syncState(d.pivotBlock.Root())

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
			log.Info("Sync completed with", "err", sync.err)
			d.bufferLock.Lock() // unlocked in Close()
			return sync.err
		case <-ctx.Done():
			log.Warn("Sync interrupted by context", "err", ctx.Err())
			close(d.quitCh)
			return ctx.Err()
		case <-d.quitCh: // currently only triggered by queue overflow
			log.Warn("Sync interrupted by quit channel")
			return errors.New("Snap sync interrupted by quit channel")
		case newPivot := <-d.newPivot:
			// If a new pivot block is found, cancel the current state sync and
			// start a new one.
			log.Debug("Pivot block updated to", "hash", d.Pivot().Root(), "height", d.Pivot().NumberU64())
			sync.Cancel()
			sync = d.syncState(newPivot.Root())
		}
	}
}

// syncState starts downloading state with the given root hash.
func (d *Downloader) syncState(root common.Hash) *stateSync {
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
func (d *Downloader) runStateSync(s *stateSync) *stateSync {
	log.Debug("State sync starting", "root", s.root)

	go s.run()
	defer s.Cancel()

	for {
		select {
		case next := <-d.stateSyncStart:
			return next
		case <-d.quitCh:
			return nil
		case <-s.done:
			return nil
		}
	}
}

// stateSync schedules requests for downloading a particular state trie defined
// by a given state root.
type stateSync struct {
	d    *Downloader // Downloader instance to access and manage current peerset
	root common.Hash // State root currently being synced

	started    chan struct{} // Started is signalled once the sync loop starts
	cancel     chan struct{} // Channel to signal a termination request
	cancelOnce sync.Once     // Ensures cancel only ever gets called once
	done       chan struct{} // Channel to signal termination completion
	err        error         // Any error hit during sync (set before completion)
}

// newStateSync creates a new state trie download scheduler. This method does not
// yet start the sync. The user needs to call run to initiate.
func newStateSync(d *Downloader, root common.Hash) *stateSync {
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
	s.err = s.d.SnapSyncer.Sync(s.root, s.cancel)
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

// DeliverSnapPacket is invoked from a peer's message handler when it transmits a
// data packet for the local node to consume.
func (d *Downloader) DeliverSnapPacket(peer *snap.Peer, packet snap.Packet) error {
	switch packet := packet.(type) {
	case *snap.AccountRangePacket:
		hashes, accounts, err := packet.Unpack()
		if err != nil {
			return err
		}
		return d.SnapSyncer.OnAccounts(peer, packet.ID, hashes, accounts, packet.Proof)

	case *snap.StorageRangesPacket:
		hashset, slotset := packet.Unpack()
		return d.SnapSyncer.OnStorage(peer, packet.ID, hashset, slotset, packet.Proof)

	case *snap.ByteCodesPacket:
		return d.SnapSyncer.OnByteCodes(peer, packet.ID, packet.Codes)

	case *snap.TrieNodesPacket:
		return d.SnapSyncer.OnTrieNodes(peer, packet.ID, packet.Nodes)

	default:
		return fmt.Errorf("unexpected snap packet type: %T", packet)
	}
}
