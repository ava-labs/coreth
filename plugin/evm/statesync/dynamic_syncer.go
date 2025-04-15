// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	// xsync "github.com/ava-labs/avalanchego/x/sync"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

type SyncBlockRequest uint8

const (
	// Dynamic state switches state root occasionally
	PivotInterval = 128
)

var (
	_ manager = &snapManager{}
	// _ manager = &xsync.Manager{} TODO: ensure this matches
)

// Interface provided by x/sync
type manager interface {
	// Stops syncing progress
	Close()

	// Non-nil if a fatal error occurred
	Error() error

	// Initiates state sync in the background
	Start(ctx context.Context) error

	// UpdateSyncTarget updates the sync target to the given root hash.
	// Can be called concurrently, but we will not do that
	UpdateSyncTarget(syncTargetRoot common.Hash) error

	// Wait blocks until one of the following occurs:
	// - sync is complete.
	// - sync fatally errored.
	// - [ctx] is canceled.
	// If [ctx] is canceled, returns [ctx].Err().
	Wait(ctx context.Context) error
}

type DynamicSyncConfig struct {
	// ChainDB is the database that the downloader will use to store the synced state
	ChainDB ethdb.Database
	// FirstPivotBlock is the block that the downloader will use as the pivot block
	FirstPivotBlock *types.Block
	// Scheme is the state scheme that the downloader will use to store the synced state
	Scheme string
	// StateSyncNodes is the list of nodes that will be used to sync the state
	StateSyncNodes []ids.NodeID
	// Network is the network that the downloader will use to connect to other nodes
	Network peer.Network
	// PivotChan is the channel that will be used to receive new pivot blocks
	PivotChan chan *types.Block
}

type DynamicSyncer struct {
	*DynamicSyncConfig
	manager manager

	// Note the pivot block does not need to be locked as it is only updated during queueing,
	// which is protected by queue lock. This may deserve its own lock for readability
	pivotBlock *types.Block

	done   chan error
	quitCh chan struct{}
}

func NewDynamicSyncer(config *DynamicSyncConfig) (*DynamicSyncer, error) {
	parsedScheme, err := rawdb.ParseStateScheme(config.Scheme, config.ChainDB)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state scheme: %w", err)
	}

	d := &DynamicSyncer{
		DynamicSyncConfig: config,
		pivotBlock:        config.FirstPivotBlock,
		done:              make(chan error),
		quitCh:            make(chan struct{}),
	}
	d.Scheme = parsedScheme

	if parsedScheme == rawdb.PathScheme || parsedScheme == rawdb.HashScheme {
		d.manager = NewSnapManager(d)
	} else {
		return nil, fmt.Errorf("unsupported database type: %s", parsedScheme)
	}

	return d, nil
}

// Starts manager in background with `ctx` context
// If manager doesn't start, will not update done channel
func (d *DynamicSyncer) Start(ctx context.Context) error {
	if err := d.manager.Start(ctx); err != nil {
		log.Error("Failed to start manager", "err", err)
		return err
	}

	// Connect to the pivot block channel
	go d.pivotFetcher(ctx)

	// Update done channel on finish
	go func() {
		err := d.manager.Wait(ctx)
		close(d.quitCh)
		d.done <- err
	}()
	return nil
}

func (d *DynamicSyncer) pivotFetcher(ctx context.Context) {
	for {
		select {
		case block := <-d.PivotChan:
			if block == nil {
				// should never happen
				// but if it does, log and continue
				log.Error("Received nil block from pivot channel")
				continue
			}
			if err := d.manager.UpdateSyncTarget(block.Root()); err != nil {
				// should never happen - will cause fatal error eventually
				log.Crit("Failed to update sync target", "err", err)
				d.manager.Close()
				continue
			}
			d.pivotBlock = block
			log.Debug("Updated pivot block", "block", block.Hash(), "height", block.NumberU64())
		case <-ctx.Done():
			log.Debug("Context done, stopping pivot channel")
			return
		case <-d.quitCh:
			log.Debug("Dynamic syncer quit, stopping pivot fetcher")
			return
		}
	}
}

func (d *DynamicSyncer) Done() <-chan error {
	return d.done
}

// Returns the current pivot block's hash
// TODO: this should maybe be locked, but with current usage it's unnecessary
func (d *DynamicSyncer) Hash() common.Hash {
	return d.pivotBlock.Hash()
}
