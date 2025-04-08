// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/peer"
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

	// Dynamic state switches state root occasionally
	// Buffer must be large enough to
	pivotInterval = 128
	bufferSize    = 3 * pivotInterval
)

var (
	queueOverflowError = errors.New("Snap sync queue overflow")

	_ manager = &snapManager{}
)

// var _ Downloader = &downloader{}

type queueElement struct {
	block    *types.Block
	req      SyncBlockRequest
	resolver func() error
}

// Interface used by x/sync
type manager interface {
	// Stops syncing progress
	Close()

	// Nonnil if a fatal error occurred
	Error() error

	// Initiates state sync in the background
	Start(ctx context.Context) error

	// UpdateSyncTarget updates the sync target to the given root hash.
	// Can be called concurrently?
	UpdateSyncTarget(syncTargetRoot common.Hash) error

	// Wait blocks until one of the following occurs:
	// - sync is complete.
	// - sync fatally errored.
	// - [ctx] is canceled.
	// If [ctx] is canceled, returns [ctx].Err().
	Wait(ctx context.Context) error
}

// Downloader is the interface that manages queueing and processing of blocks
// and updating the pivot block.
// type Downloader interface {
// 	// Returns the current pivot
// 	Pivot() *types.Block
// 	// Opens bufferLock to allow block requests to go through after finalizing the sync
// 	Close()
// 	// QueueBlock queues a block for processing by the state syncer.
// 	QueueBlockOrPivot(b *types.Block, req SyncBlockRequest, resolver func() error) error
// 	// It controls the synchronisation of state nodes of the pivot block.
// 	Start(ctx context.Context) error
// 	// Done returns a channel that is closed when the downloader is done
// 	Done() <-chan error
// 	// RegisterSyncNodes registers the state sync nodes to the network
// 	RegisterSyncNodes(network peer.Network, stateSyncNodes []ids.NodeID) error
// 	// DeliverSnapPacket is invoked from a peer's message handler when it transmits a
// 	// data packet for the local node to consume.
// 	DeliverSnapPacket(peer *snap.Peer, packet snap.Packet) error
// }

type DynamicSyncConfig struct {
	// ChainDB is the database that the downloader will use to store the synced state
	ChainDB ethdb.Database
	// PivotBlock is the block that the downloader will use as the pivot block
	FirstPivotBlock *types.Block
	// Scheme is the state scheme that the downloader will use to store the synced state
	Scheme string
	// SyncQueueLock is the lock that will be used to protect the block buffer
	SyncQueueLock *sync.Mutex
	// StateSyncNodes is the list of nodes that will be used to sync the state
	StateSyncNodes []ids.NodeID
	// Network is the network that the downloader will use to connect to other nodes
	Network peer.Network
	// SyncType is the type of sync that will be used
	SyncType string
}

type DynamicSyncer struct {
	*DynamicSyncConfig
	manager manager

	// Note the pivot block does not need to be locked as it is only updated during queueing,
	// which is protected by avalnchego's lock
	pivotBlock  *types.Block
	blockBuffer []*queueElement
	bufferLen   int

	newPivot chan *types.Block
	done     chan error
}

func NewDynamicSyncer(config *DynamicSyncConfig) (*DynamicSyncer, error) {
	_, err := rawdb.ParseStateScheme(config.Scheme, config.ChainDB)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state scheme: %w", err)
	}

	d := &DynamicSyncer{
		blockBuffer:       make([]*queueElement, bufferSize),
		newPivot:          make(chan *types.Block),
		done:              make(chan error),
		DynamicSyncConfig: config,
	}

	if config.SyncType == "snap" {
		d.manager = NewSnapManager(d)
	} else {
		return nil, fmt.Errorf("unsupported sync type: %s", config.SyncType)
	}

	return d, nil
}

// Implement Syncer interface
func (d *DynamicSyncer) Start(ctx context.Context) error {
	if err := d.manager.Start(ctx); err != nil {
		log.Error("Failed to start manager", "err", err)
		d.done <- err
		return err
	}

	go func() {
		err := d.manager.Wait(ctx)
		d.done <- err
	}()
	return nil
}

func (d *DynamicSyncer) Done() <-chan error {
	return d.done
}

// Returns the current pivot
func (d *DynamicSyncer) Pivot() *types.Block {
	return d.pivotBlock
}

// Opens bufferLock to allow block requests to go through after finalizing the sync
func (d *DynamicSyncer) Close() error {
	if err := d.flushQueue(true); err != nil {
		return fmt.Errorf("failed to flush queue: %w", err)
	}
	d.SyncQueueLock.Unlock()
	return nil
}

// QueueBlock queues a block for processing by the state syncer.
// This assumes the queue lock is NOT held
func (d *DynamicSyncer) QueueBlockOrPivot(b *types.Block, req SyncBlockRequest, resolver func() error) error {
	d.SyncQueueLock.Lock()
	defer d.SyncQueueLock.Unlock()
	// Check there's space in the queue
	if d.bufferLen >= len(d.blockBuffer) {
		d.manager.Close()
		err := queueOverflowError
		d.done <- err
		return err
	}

	d.blockBuffer[d.bufferLen] = &queueElement{b, req, resolver}
	d.bufferLen++

	log.Debug("Received queue request", "hash", b.Hash(), "height", b.Number(), "req type", req)

	// If on pivot interval, we should pivot (regardless of whether the queue is full)
	if req == AcceptSyncBlockRequest && b.NumberU64()%pivotInterval == 0 {
		log.Debug("Found new pivot block", "hash", b.Hash(), "height", b.NumberU64())
		if b.NumberU64() <= d.pivotBlock.NumberU64() {
			// Should never happen, attempt to handle
			log.Warn("Received pivot with height <= pivot block", "old hash", b.Hash(), "old height")
		}

		// Reset pivot first in other goroutine
		d.pivotBlock = b
		if err := d.manager.UpdateSyncTarget(b.Root()); err != nil {
			return fmt.Errorf("failed to update sync target: %w", err)
		}

		// Clear queue
		if err := d.flushQueue(false); err != nil {
			log.Error("Issue flushing queue", "err", err)
			d.manager.Close()
			d.done <- err
		}
	}

	return nil
}

// Clears queue of blocks. Assumes no elements are past pivot and bufferLock is held
// If `final`, executes blocks only eth operations. Otherwise executes only atomic operations
func (d *DynamicSyncer) flushQueue(final bool) error {
	newLength := 0
	log.Debug("Flushing queue", "final", final, "bufferLen", d.bufferLen)
	defer func() {
		d.bufferLen = newLength
	}()

	if final {
		// We should execute all blocks if final
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

	// We should only remove blocks earlier than the pivot
	for i, elem := range d.blockBuffer {
		if i >= d.bufferLen {
			return nil
		}

		// TODO: This *shouldn't* cause a race, but obvious protection would be nice
		if elem.block.NumberU64() > d.pivotBlock.NumberU64() {
			d.blockBuffer[newLength] = elem
			newLength++
		}
	}

	return nil
}
