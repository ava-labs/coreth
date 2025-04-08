// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: This may need go-ethereum license

package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/eth/protocols/snap"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
)

type snapManager struct {
	// The peer network instance
	network peer.Network
	// The state syncer instance
	snapSyncer *snap.Syncer
	// The pivot block
	pivotRoot common.Hash
	// Track errors
	fatalError error
	errorLock  sync.Mutex

	// The channel to signal when the state sync starts
	stateSyncStart chan *stateSync
	// Signals new pivot block
	newPivot chan common.Hash
	// The channel to signal when the state sync is done
	quitCh chan struct{}
}

// NewSnapManager creates a new snap manager instance.
func NewSnapManager(d *DynamicSyncer) *snapManager {
	m := &snapManager{
		network:        d.Network,
		snapSyncer:     snap.NewSyncer(d.ChainDB, d.Scheme),
		pivotRoot:      d.FirstPivotBlock.Root(),
		stateSyncStart: make(chan *stateSync),
		quitCh:         make(chan struct{}),
	}

	m.registerSnapSyncNodes(m.network, d.StateSyncNodes)
	go m.stateFetcher()

	return m
}

// stateFetcher manages the active state sync and accepts requests
// on its behalf.
func (m *snapManager) stateFetcher() {
	for {
		select {
		case s := <-m.stateSyncStart:
			for next := s; next != nil; {
				next = m.runStateSync(next)
			}
		case <-m.quitCh:
			return
		}
	}
}

// Start starts the snap manager and initiates state sync.
func (m *snapManager) Start(ctx context.Context) error {
	go m.snapSync(ctx)
	return nil
}

func (m *snapManager) Close() {
	close(m.quitCh)
}

func (m *snapManager) Wait(ctx context.Context) error {
	for {
		select {
		case <-m.quitCh:
			m.errorLock.Lock()
			defer m.errorLock.Unlock()
			return m.fatalError
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *snapManager) Error() error {
	m.errorLock.Lock()
	defer m.errorLock.Unlock()
	return m.fatalError
}

func (m *snapManager) UpdateSyncTarget(root common.Hash) error {
	m.newPivot <- root
	return nil
}

// sync takes fetch results from the queue and writes them to the
// database. It also controls the synchronisation of state nodes of the pivot block.
func (m *snapManager) snapSync(ctx context.Context) {
	// Start syncing state of the reported head block. This should get us most of
	// the state of the pivot block.
	sync := m.syncState(m.pivotRoot)

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
			m.errorLock.Lock()
			m.fatalError = sync.err
			defer m.errorLock.Unlock()
			close(m.quitCh)
			return
		case <-ctx.Done():
			log.Error("Sync interrupted by context", "err", ctx.Err())
			close(m.quitCh)
			// No need to report error
			return
		case <-m.quitCh:
			log.Warn("Sync canceled externally")
			return
		case newPivot := <-m.newPivot:
			sync.Cancel()
			sync = m.syncState(newPivot)
		}
	}
}

// syncState starts downloading state with the given root hash.
func (m *snapManager) syncState(root common.Hash) *stateSync {
	// Create the state sync
	s := newStateSync(m, root)
	select {
	case m.stateSyncStart <- s:
		// If we tell the statesync to restart with a new root, we also need
		// to wait for it to actually also start -- when old requests have timed
		// out or been delivered
		<-s.started
	case <-m.quitCh:
		s.err = errors.New("errCancelStateFetch") //errCancelStateFetch from geth
		close(s.done)
	}
	return s
}

// runStateSync runs a state synchronisation until it completes or another root
// hash is requested to be switched over to.
func (m *snapManager) runStateSync(s *stateSync) *stateSync {
	log.Debug("State sync starting", "root", s.root)

	go s.run()
	defer s.Cancel()

	for {
		select {
		case next := <-m.stateSyncStart:
			return next
		case <-m.quitCh:
			return nil
		case <-s.done:
			return nil
		}
	}
}

func (m *snapManager) registerSnapSyncNodes(network peer.Network, stateSyncNodes []ids.NodeID) error {
	p2pClient := network.NewClient(ProtocolID)
	if len(stateSyncNodes) > 0 {
		for _, nodeID := range stateSyncNodes {
			if err := m.snapSyncer.Register(NewOutboundPeer(nodeID, m, p2pClient)); err != nil {
				return err
			}
		}
	} else {
		if err := network.AddConnector(NewConnector(m, p2pClient)); err != nil {
			return err
		}
	}
	return nil
}

// DeliverSnapPacket is invoked from a peer's message handler when it transmits a
// data packet for the local node to consume.
func (m *snapManager) DeliverSnapPacket(peer *snap.Peer, packet snap.Packet) error {
	switch packet := packet.(type) {
	case *snap.AccountRangePacket:
		hashes, accounts, err := packet.Unpack()
		if err != nil {
			return err
		}
		return m.snapSyncer.OnAccounts(peer, packet.ID, hashes, accounts, packet.Proof)

	case *snap.StorageRangesPacket:
		hashset, slotset := packet.Unpack()
		return m.snapSyncer.OnStorage(peer, packet.ID, hashset, slotset, packet.Proof)

	case *snap.ByteCodesPacket:
		return m.snapSyncer.OnByteCodes(peer, packet.ID, packet.Codes)

	case *snap.TrieNodesPacket:
		return m.snapSyncer.OnTrieNodes(peer, packet.ID, packet.Nodes)

	default:
		return fmt.Errorf("unexpected snap packet type: %T", packet)
	}
}

// stateSync schedules requests for downloading a particular state trie defined
// by a given state root.
type stateSync struct {
	m    *snapManager // Downloader instance to access and manage current peerset
	root common.Hash  // State root currently being synced

	started    chan struct{} // Started is signalled once the sync loop starts
	cancel     chan struct{} // Channel to signal a termination request
	cancelOnce sync.Once     // Ensures cancel only ever gets called once
	done       chan struct{} // Channel to signal termination completion
	err        error         // Any error hit during sync (set before completion)
}

// newStateSync creates a new state trie download scheduler. This method does not
// yet start the sync. The user needs to call run to initiate.
func newStateSync(m *snapManager, root common.Hash) *stateSync {
	return &stateSync{
		m:       m,
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
	s.err = s.m.snapSyncer.Sync(s.root, s.cancel)
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
