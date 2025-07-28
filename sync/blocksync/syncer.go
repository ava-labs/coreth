// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocksync

import (
	"context"
	"errors"
	"fmt"

	synccommon "github.com/ava-labs/coreth/sync"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

var (
	_ synccommon.Syncer = (*blockSyncer)(nil)

	errNilClient       = errors.New("Client cannot be nil")
	errNilDatabase     = errors.New("Database cannot be nil")
	errInvalidFromHash = errors.New("FromHash cannot be empty")
)

type BlockSyncerConfig struct {
	ChainDB      ethdb.Database
	Client       statesyncclient.Client
	FromHash     common.Hash
	FromHeight   uint64
	ParentsToGet uint64
}

func (c *BlockSyncerConfig) Validate() error {
	if c.ChainDB == nil {
		return errNilDatabase
	}
	if c.Client == nil {
		return errNilClient
	}
	if c.FromHash == (common.Hash{}) {
		return errInvalidFromHash
	}
	return nil
}

type blockSyncer struct {
	*BlockSyncerConfig

	err    chan error
	cancel context.CancelFunc
}

func NewBlockSyncer(config *BlockSyncerConfig) (*blockSyncer, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &blockSyncer{
		BlockSyncerConfig: config,
		err:               make(chan error),
	}, nil
}

func (syncer *blockSyncer) Start(ctx context.Context) error {
	cancelCtx, cancel := context.WithCancel(ctx)
	syncer.cancel = cancel
	go func() {
		syncer.err <- syncer.syncBlocks(cancelCtx)
	}()
	return nil
}

func (syncer *blockSyncer) Wait(ctx context.Context) error {
	if syncer.cancel == nil {
		return synccommon.ErrWaitBeforeStart
	}

	select {
	case err := <-syncer.err:
		return err
	case <-ctx.Done():
		syncer.cancel()
		<-syncer.err // wait for the syncer to finish
		return ctx.Err()
	}
}

// syncBlocks fetches (up to) [parentsToGet] blocks from peers
// using [client] and writes them to disk.
// the process begins with [fromHash] and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
func (syncer *blockSyncer) syncBlocks(ctx context.Context) error {
	nextHash := syncer.FromHash
	nextHeight := syncer.FromHeight
	parentsPerRequest := uint16(32)

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for syncer.ParentsToGet >= 0 {
		blk := rawdb.ReadBlock(syncer.ChainDB, nextHash, nextHeight)
		if blk != nil {
			// block exists
			nextHash = blk.ParentHash()
			nextHeight--
			syncer.ParentsToGet--
			continue
		}

		// block was not found
		break
	}

	// get any blocks we couldn't find on disk from peers and write
	// them to disk.
	batch := syncer.ChainDB.NewBatch()
	for i := syncer.ParentsToGet - 1; i >= 0 && (nextHash != common.Hash{}); {
		if err := ctx.Err(); err != nil {
			return err
		}
		blocks, err := syncer.Client.GetBlocks(ctx, nextHash, nextHeight, parentsPerRequest)
		if err != nil {
			return fmt.Errorf("could not get blocks from peer: err: %w, nextHash: %s, remaining: %d", err, nextHash, i+1)
		}
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

			i--
			nextHash = block.ParentHash()
			nextHeight--
		}
		log.Info("fetching blocks from peer", "remaining", i+1, "total", syncer.ParentsToGet)
	}
	log.Info("fetched blocks from peer", "total", syncer.ParentsToGet)
	return batch.Write()
}
