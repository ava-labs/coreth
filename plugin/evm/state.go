// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ethereum/go-ethereum/log"
)

// Define db prefixes and keys
var (
	BlockByHeightPrefix = []byte("blockByHeight")
	LastAcceptedKey     = []byte("lastAccepted")
)

// UnmarshalType ...
type UnmarshalType func(b []byte) (snowman.Block, error)

// BuildBlockType ...
type BuildBlockType func() (snowman.Block, error)

// GetBlockType ...
type GetBlockType func(blkID ids.ID) (snowman.Block, error)

// ChainState defines the canonical state of the chain
// it tracks the accepted blocks and wraps a VM's implementation
// of snowman.Block in order to take care of writing blocks to
// the database and adding a caching layer for both the blocks
// and their statuses.
type ChainState struct {
	baseDB                 *versiondb.Database
	acceptedBlocksByHeight database.Database

	getBlock       GetBlockType
	unmarshalBlock UnmarshalType
	buildBlock     BuildBlockType
	genesisBlock   snowman.Block

	// Caches of processing and decided blocks
	// Invariant: any block in cache will always
	// be a wrapped block.
	lock              sync.Mutex
	processingBlocks  map[ids.ID]*BlockWrapper
	decidedBlocks     cache.LRU
	missingBlocks     cache.LRU
	lastAcceptedBlock *BlockWrapper
}

// NewChainState ...
func NewChainState(baseDB *versiondb.Database, genesisBlock snowman.Block, getBlock GetBlockType, unmarshalBlock UnmarshalType, buildBlock BuildBlockType, cacheSize int) (*ChainState, error) {
	acceptedBlocksByHeightDB := prefixdb.New(BlockByHeightPrefix, baseDB)
	lastAcceptedIDBytes, err := baseDB.Get(LastAcceptedKey)
	var lastAcceptedBlock *BlockWrapper
	if err == nil {
		lastAcceptedID, err := ids.ToID(lastAcceptedIDBytes)
		if err != nil {
			return nil, err
		}
		internalBlock, err := getBlock(lastAcceptedID)
		if err != nil {
			return nil, err
		}
		lastAcceptedBlock = &BlockWrapper{
			Block:  internalBlock,
			status: choices.Accepted,
		}
	} else {
		genesisID := genesisBlock.ID()
		if err := baseDB.Put(LastAcceptedKey, genesisID[:]); err != nil {
			return nil, err
		}
		heightKey := make([]byte, 8)
		binary.BigEndian.PutUint64(heightKey, genesisBlock.Height())
		if err := acceptedBlocksByHeightDB.Put(heightKey, genesisID[:]); err != nil {
			return nil, err
		}
		lastAcceptedBlock = &BlockWrapper{
			Block:  genesisBlock,
			status: choices.Accepted,
		}
	}

	state := &ChainState{
		baseDB:                 baseDB,
		acceptedBlocksByHeight: acceptedBlocksByHeightDB,
		getBlock:               getBlock,
		unmarshalBlock:         unmarshalBlock,
		buildBlock:             buildBlock,
		processingBlocks:       make(map[ids.ID]*BlockWrapper, cacheSize),
		decidedBlocks:          cache.LRU{Size: cacheSize},
		missingBlocks:          cache.LRU{Size: cacheSize},
		lastAcceptedBlock:      lastAcceptedBlock,
	}
	lastAcceptedBlock.state = state
	state.decidedBlocks.Put(lastAcceptedBlock.ID(), lastAcceptedBlock)
	return state, nil
}

// GetBlock ...
func (c *ChainState) GetBlock(blkID ids.ID) (snowman.Block, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if blk, ok := c.processingBlocks[blkID]; ok {
		return blk, nil
	}

	if blk, ok := c.decidedBlocks.Get(blkID); ok {
		return blk.(snowman.Block), nil
	}

	if _, ok := c.missingBlocks.Get(blkID); ok {
		return nil, errors.New("unknown block")
	}

	blk, err := c.getBlock(blkID)
	if err != nil {
		c.missingBlocks.Put(blkID, struct{}{})
		return nil, err
	}

	return c.AddBlock(blk)
}

// ParseBlock ...
func (c *ChainState) ParseBlock(b []byte) (snowman.Block, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	blkID := hashing.ComputeHash256Array(b)

	// Check caching layers
	if blk, ok := c.processingBlocks[blkID]; ok {
		return blk, nil
	}
	if blk, ok := c.decidedBlocks.Get(blkID); ok {
		return blk.(snowman.Block), nil
	}

	blk, err := c.unmarshalBlock(b)
	if err != nil {
		return nil, err
	}
	c.missingBlocks.Evict(blk.ID())

	return c.AddBlock(blk)
}

// BuildBlock ...
func (c *ChainState) BuildBlock() (snowman.Block, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	log.Info("Calling internal buildBlock")
	blk, err := c.buildBlock()
	if err != nil {
		return nil, err
	}
	log.Info("buildBlock returned")
	c.missingBlocks.Evict(blk.ID())

	// Blocks built by BuildBlock are built on top of the
	// preferred block, so they are guaranteed to be processing
	wrappedBlk := &BlockWrapper{
		Block:  blk,
		status: choices.Processing,
		state:  c,
	}
	c.processingBlocks[blk.ID()] = wrappedBlk

	return wrappedBlk, nil
}

// AddBlock adds [blk] to processing and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block
func (c *ChainState) AddBlock(blk snowman.Block) (snowman.Block, error) {
	wrappedBlk := &BlockWrapper{
		Block:  blk,
		status: choices.Processing,
		state:  c,
	}

	status, err := c.getStatus(blk)
	if err != nil {
		return nil, err
	}

	wrappedBlk.status = status
	blkID := blk.ID()
	switch status {
	case choices.Accepted, choices.Rejected:
		c.decidedBlocks.Put(blkID, wrappedBlk)
	case choices.Processing:
		c.processingBlocks[blkID] = wrappedBlk
	default:
		return nil, fmt.Errorf("found unexpected status for blk %s: %s", blkID, status)
	}

	return wrappedBlk, nil
}

// LastAccepted ...
func (c *ChainState) LastAccepted() ids.ID {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.lastAcceptedBlock.ID()
}

// LastAcceptedBlock returns the last accepted wrapped block
func (c *ChainState) LastAcceptedBlock() *BlockWrapper {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.lastAcceptedBlock
}

// getStatus returns the status of [blk]
func (c *ChainState) getStatus(blk snowman.Block) (choices.Status, error) {
	if blk.Height() > c.lastAcceptedBlock.Height() {
		return choices.Processing, nil
	}

	heightKey := make([]byte, 8)
	binary.BigEndian.PutUint64(heightKey, blk.Height())
	blkIDAtHeight, err := c.acceptedBlocksByHeight.Get(heightKey)
	if err != nil {
		return choices.Unknown, fmt.Errorf("failed to get acceptedID at height %d, below last accepted height: %w", blk.Height(), err)
	}

	acceptedID, err := ids.ToID(blkIDAtHeight)
	if err != nil {
		return choices.Unknown, fmt.Errorf("failed to convert accepted ID at block height %d to an ID: %w", blk.Height(), err)
	}
	if acceptedID == blk.ID() {
		return choices.Accepted, nil
	}

	return choices.Rejected, nil
}

// BlockWrapper wraps a snowman Block and adds a caching layer
// to keep processing blocks in memory and an LRU cache of decided
// blocks.
type BlockWrapper struct {
	snowman.Block
	status choices.Status

	state *ChainState
}

// Accept ...
func (bw *BlockWrapper) Accept() error {
	bw.state.lock.Lock()
	defer bw.state.lock.Unlock()

	blkID := bw.ID()
	delete(bw.state.processingBlocks, blkID)
	bw.state.decidedBlocks.Put(blkID, bw)
	bw.status = choices.Accepted
	bw.state.lastAcceptedBlock = bw
	if err := bw.state.baseDB.Put(LastAcceptedKey, blkID[:]); err != nil {
		return err
	}

	heightKey := make([]byte, 8)
	binary.BigEndian.PutUint64(heightKey, bw.Height())

	if err := bw.state.acceptedBlocksByHeight.Put(heightKey, blkID[:]); err != nil {
		return err
	}
	if err := bw.Block.Accept(); err != nil {
		return err
	}

	return bw.state.baseDB.Commit()
}

// Reject ...
func (bw *BlockWrapper) Reject() error {
	bw.state.lock.Lock()
	defer bw.state.lock.Unlock()

	delete(bw.state.processingBlocks, bw.ID())
	bw.state.decidedBlocks.Put(bw.ID(), bw)
	bw.status = choices.Rejected
	return bw.Block.Reject()
}

// Status ...
func (bw *BlockWrapper) Status() choices.Status {
	return bw.status
}
