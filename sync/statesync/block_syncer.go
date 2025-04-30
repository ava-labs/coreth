// (c) 2021-2025, Ava Labs, Inc. All rights reserved.

package statesync

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

const (
	// State sync fetches [ParentsToGet] parents of the block it syncs to.
	// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
	ParentsToGet = 256
)

type blockSyncer struct {
	*BlockSyncerConfig

	// cancel is used to cancel the syncer
	cancel     context.CancelFunc
	cancelOnce sync.Once
	result     chan error
}

type BlockSyncerConfig struct {
	Client  syncclient.Client
	ChainDB ethdb.Database
}

func NewBlockSyncer(config *BlockSyncerConfig) (*blockSyncer, error) {
	return &blockSyncer{
		BlockSyncerConfig: config,
		result:            make(chan error),
	}, nil
}

func (syncer *blockSyncer) Start(ctx context.Context, target *message.SyncSummary) error {
	ctx, cancel := context.WithCancel(ctx)
	syncer.cancel = cancel
	go func() {
		syncer.result <- syncer.syncBlocks(ctx, target)
	}()
	return nil
}

func (syncer *blockSyncer) syncBlocks(ctx context.Context, target *message.SyncSummary) error {
	nextHash := target.BlockHash
	nextHeight := target.BlockNumber
	parentsPerRequest := uint16(32)
	parentsToGet := ParentsToGet // rescope out of const

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for parentsToGet >= 0 {
		blk := rawdb.ReadBlock(syncer.ChainDB, nextHash, nextHeight)
		if blk != nil {
			// block exists
			nextHash = blk.ParentHash()
			nextHeight--
			parentsToGet--
			continue
		}

		// block was not found
		break
	}

	// get any blocks we couldn't find on disk from peers and write
	// them to disk.
	batch := syncer.ChainDB.NewBatch()
	for i := parentsToGet - 1; i >= 0 && (nextHash != common.Hash{}); {
		if err := ctx.Err(); err != nil {
			return err
		}
		blocks, err := syncer.Client.GetBlocks(ctx, nextHash, nextHeight, parentsPerRequest)
		if err != nil {
			log.Error("could not get blocks from peer", "err", err, "nextHash", nextHash, "remaining", i+1)
			return err
		}
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

			i--
			nextHash = block.ParentHash()
			nextHeight--
		}
		log.Info("fetching blocks from peer", "remaining", i+1, "total", parentsToGet)
	}
	log.Info("fetched blocks from peer", "total", parentsToGet)
	return batch.Write()
}

func (syncer *blockSyncer) Wait(ctx context.Context) error {
	return <-syncer.result
}

func (syncer *blockSyncer) Close() error {
	syncer.cancelOnce.Do(func() {
		syncer.cancel()
	})
	return nil
}

func (syncer *blockSyncer) UpdateSyncTarget(ctx context.Context, target *message.SyncSummary) error {
	return errors.New("block syncer does not support updating sync target")
}
