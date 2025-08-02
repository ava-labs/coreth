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

	errNilClient                     = errors.New("Client cannot be nil")
	errNilDatabase                   = errors.New("Database cannot be nil")
	errInvalidFromHash               = errors.New("FromHash cannot be empty")
	errParentsToGetExceedsFromHeight = errors.New("ParentsToGet cannot exceed FromHeight")
)

type Config struct {
	ChainDB      ethdb.Database
	Client       statesyncclient.Client
	FromHash     common.Hash
	FromHeight   uint64
	ParentsToGet uint64
}

func (c *Config) Validate() error {
	if c.ChainDB == nil {
		return errNilDatabase
	}
	if c.Client == nil {
		return errNilClient
	}
	if c.FromHash == (common.Hash{}) {
		return errInvalidFromHash
	}
	if c.ParentsToGet > c.FromHeight {
		return errParentsToGetExceedsFromHeight
	}
	return nil
}

type blockSyncer struct {
	config *Config

	err    chan error
	cancel context.CancelFunc
}

func NewSyncer(config *Config) (*blockSyncer, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &blockSyncer{
		config: config,
		err:    make(chan error),
	}, nil
}

func (s *blockSyncer) Start(ctx context.Context) error {
	cancelCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		s.err <- s.sync(cancelCtx)
	}()
	return nil
}

func (s *blockSyncer) Wait(ctx context.Context) error {
	if s.cancel == nil {
		return synccommon.ErrWaitBeforeStart
	}

	select {
	case err := <-s.err:
		return err
	case <-ctx.Done():
		s.cancel()
		<-s.err // wait for the syncer to finish
		return ctx.Err()
	}
}

// sync fetches (up to) [ParentsToGet] blocks from peers
// using [Client] and writes them to disk.
// the process begins with [FromHash] and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
//
// TODO: we will always fetch all the blocks, even if some blocks were
// available on disk. This can happen if only the most recent block is
// not available.
func (s *blockSyncer) sync(ctx context.Context) error {
	nextHash := s.config.FromHash
	nextHeight := s.config.FromHeight
	parentsToGet := int64(s.config.ParentsToGet)
	parentsPerRequest := uint16(32)

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for parentsToGet >= 0 {
		blk := rawdb.ReadBlock(s.config.ChainDB, nextHash, nextHeight)
		if blk == nil {
			// block was not found
			break
		}

		// block exists
		nextHash = blk.ParentHash()
		nextHeight--
		parentsToGet--
	}

	// get any blocks we couldn't find on disk from peers and write
	// them to disk.
	batch := s.config.ChainDB.NewBatch()
	for i := parentsToGet - 1; i >= 0 && (nextHash != common.Hash{}); {
		if err := ctx.Err(); err != nil {
			return err
		}
		blocks, err := s.config.Client.GetBlocks(ctx, nextHash, nextHeight, parentsPerRequest)
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
		log.Info("fetching blocks from peer", "remaining", i+1, "total", parentsToGet)
	}
	log.Info("fetched blocks from peer", "total", parentsToGet)
	return batch.Write()
}
