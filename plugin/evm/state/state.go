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
)

// Define db prefixes and keys
var (
	BlockByHeightPrefix      = []byte("blockByHeight")
	LastAcceptedKey          = []byte("lastAccepted")
	chainStateInternalPrefix = []byte("internal")
	chainStateExternalPrefix = []byte("external")
)

var (
	errUnknownBlock = errors.New("unknown block")
)

// UnmarshalType ...
type UnmarshalType func([]byte) (Block, error)

// BuildBlockType ...
type BuildBlockType func() (Block, error)

// GetBlockType ...
type GetBlockType func(ids.ID) (Block, error)

// ChainState defines the canonical state of the chain
// it tracks the accepted blocks and wraps a VM's implementation
// of snowman.Block in order to take care of writing blocks to
// the database and adding a caching layer for both the blocks
// and their statuses.
type ChainState struct {
	// baseDB is the base level database used to make
	// atomic commits on block acceptance.
	baseDB *versiondb.Database

	// internalDB is used for all storage internal to ChainState
	// which includes only the VM's canonical index of height -> blkID
	// for accepted blocks.
	internalDB database.Database
	// externalDB is the database provided by ChainState for the VM to
	// store data external to ChainState including the blocks themselves.
	externalDB database.Database
	// acceptedBlocksByHeight is prefixed from [internalDB] and provides
	// a lookup from height to the accepted blkID at that height.
	acceptedBlocksByHeight database.Database

	// ChainState keeps these function types to request operations
	// from the VM implementation.
	getBlock       GetBlockType
	unmarshalBlock UnmarshalType
	buildBlock     BuildBlockType
	genesisBlock   snowman.Block

	lock sync.Mutex
	// processingBlocks are the verified blocks that have entered
	// consensus
	processingBlocks map[ids.ID]*BlockWrapper
	// decidedBlocks is an LRU cache of decided blocks.
	decidedBlocks *cache.LRU
	// unverifiedBlocks is an LRU cache of blocks with status processing
	// that have not yet been verified.
	unverifiedBlocks *cache.LRU
	// missingBlocks is an LRU cache of missing blocks
	missingBlocks     *cache.LRU
	lastAcceptedBlock *BlockWrapper
}

// NewChainState ...
func NewChainState(db database.Database, cacheSize int) *ChainState {
	baseDB := versiondb.New(db)
	internalDB := prefixdb.New(chainStateInternalPrefix, baseDB)
	externalDB := prefixdb.New(chainStateExternalPrefix, baseDB)
	acceptedBlocksByHeightDB := prefixdb.New(BlockByHeightPrefix, internalDB)

	state := &ChainState{
		baseDB:                 baseDB,
		internalDB:             internalDB,
		externalDB:             externalDB,
		acceptedBlocksByHeight: acceptedBlocksByHeightDB,
		processingBlocks:       make(map[ids.ID]*BlockWrapper, cacheSize),
		decidedBlocks:          &cache.LRU{Size: cacheSize},
		missingBlocks:          &cache.LRU{Size: cacheSize},
		unverifiedBlocks:       &cache.LRU{Size: cacheSize},
	}
	return state
}

// Initialize sets the genesis block, last accepted block, and the functions for retrieving blocks from the VM layer.
func (c *ChainState) Initialize(genesisBlock Block, getBlock GetBlockType, unmarshalBlock UnmarshalType, buildBlock BuildBlockType) error {
	// Set the functions for retrieving blocks from the VM
	c.getBlock = getBlock
	c.unmarshalBlock = unmarshalBlock
	c.buildBlock = buildBlock

	// Initialize the last accepted block
	lastAcceptedIDBytes, err := c.internalDB.Get(LastAcceptedKey)
	var lastAcceptedBlock *BlockWrapper
	if err == nil {
		lastAcceptedID, err := ids.ToID(lastAcceptedIDBytes)
		if err != nil {
			return err
		}
		internalBlock, err := getBlock(lastAcceptedID)
		if err != nil {
			return err
		}
		lastAcceptedBlock = &BlockWrapper{
			Block:  internalBlock,
			status: choices.Accepted,
		}
	} else {
		genesisID := genesisBlock.ID()
		if err := c.internalDB.Put(LastAcceptedKey, genesisID[:]); err != nil {
			return err
		}
		if err := c.acceptedBlocksByHeight.Put(heightKey(genesisBlock.Height()), genesisID[:]); err != nil {
			return err
		}
		lastAcceptedBlock = &BlockWrapper{
			Block:  genesisBlock,
			status: choices.Accepted,
		}
	}
	lastAcceptedBlock.state = c
	c.lastAcceptedBlock = lastAcceptedBlock
	// Put the last accepted block in the decided block cache to start.
	c.decidedBlocks.Put(lastAcceptedBlock.ID(), lastAcceptedBlock)

	return nil
}

// FlushCaches flushes each block cache completely.
func (c *ChainState) FlushCaches() {
	c.decidedBlocks.Flush()
	c.missingBlocks.Flush()
	c.unverifiedBlocks.Flush()
}

// ExternalDB returns a database to be used external to ChainState
// The returned database must handle batching and atomicity by itself except for
// any operations that take place during block decisions. Any database operations
// that occur during block decisions (Accept/Reject) will be automatically batched.
func (c *ChainState) ExternalDB() database.Database { return c.externalDB }

// GetBlock returns the BlockWrapper as snowman.Block corresponding to [blkID]
func (c *ChainState) GetBlock(blkID ids.ID) (snowman.Block, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if blk, ok := c.getCachedBlock(blkID); ok {
		return blk, nil
	}

	if _, ok := c.missingBlocks.Get(blkID); ok {
		return nil, errUnknownBlock
	}

	c.lock.Unlock()
	blk, err := c.getBlock(blkID)
	c.lock.Lock()
	if err != nil {
		c.missingBlocks.Put(blkID, struct{}{})
		return nil, err
	}

	// Since this block is not in processing, we can add it as
	// a non-processing block to the correct cache.
	return c.addNonProcessingBlock(blk)
}

// getCachedBlock checks the caches for [blkID] by priority. Returning
// true if [blkID] is found in one of the caches.
func (c *ChainState) getCachedBlock(blkID ids.ID) (snowman.Block, bool) {
	if blk, ok := c.processingBlocks[blkID]; ok {
		return blk, true
	}

	if blk, ok := c.decidedBlocks.Get(blkID); ok {
		return blk.(snowman.Block), true
	}

	if blk, ok := c.unverifiedBlocks.Get(blkID); ok {
		return blk.(snowman.Block), true
	}

	return nil, false
}

// GetBlockInternal returns the internal representation of [blkID]
func (c *ChainState) GetBlockInternal(blkID ids.ID) (Block, error) {
	wrappedBlk, err := c.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	return wrappedBlk.(*BlockWrapper).Block, nil
}

// getBlockIDAtHeight returns the blkID accepted at [height] if there is
// one.
// assumes the lock is held.
func (c *ChainState) getBlockIDAtHeight(height uint64) (ids.ID, error) {
	if height > c.lastAcceptedBlock.Height() {
		return ids.ID{}, fmt.Errorf("no block has been accepted at height: %d", height)
	}

	blkIDBytes, err := c.acceptedBlocksByHeight.Get(heightKey(height))
	if err != nil {
		return ids.ID{}, err
	}

	blkID, err := ids.ToID(blkIDBytes)
	if err != nil {
		return ids.ID{}, fmt.Errorf("failed to convert accepted blockID bytes to an ID type: %w", err)
	}
	return blkID, nil
}

// ParseBlock attempts to parse [b] into an internal Block and adds it to the appropriate
// caching layer if successful.
func (c *ChainState) ParseBlock(b []byte) (snowman.Block, error) {
	blk, err := c.unmarshalBlock(b)
	if err != nil {
		return nil, err
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	blkID := blk.ID()
	// Check for an existing block, so we can return a unique block
	// if processing or simply allow this block to be immediately
	// garbage collected if it is already cached.
	// If the block is already cached, there's no need to evict from
	// missing blocks, since we already have it.
	if blk, ok := c.getCachedBlock(blkID); ok {
		return blk, nil
	}

	c.missingBlocks.Evict(blkID)

	// Since we've checked above if it's in processing we can add [blk]
	// as a non-processing block here.
	return c.addNonProcessingBlock(blk)
}

// BuildBlock attempts to build a new internal Block, wraps it, and adds it
// to the appropriate caching layer if successful.
func (c *ChainState) BuildBlock() (snowman.Block, error) {
	blk, err := c.buildBlock()
	if err != nil {
		return nil, err
	}

	// Lock after building the block, so that the VM can call into ChainState
	c.lock.Lock()
	defer c.lock.Unlock()
	// Evict the produced block from missing blocks.
	c.missingBlocks.Evict(blk.ID())

	// Blocks built by BuildBlock are built on top of the
	// preferred block, such that they are guaranteed to
	// have status Processing.
	blk.SetStatus(choices.Processing)
	wrappedBlk := &BlockWrapper{
		Block:  blk,
		status: choices.Processing,
		state:  c,
	}
	// Since the consensus engine has not called Verify on this
	// block yet, we can add it directly as a non-processing block.
	c.unverifiedBlocks.Put(blk.ID(), wrappedBlk)
	return wrappedBlk, nil
}

// addNonProcessingBlock adds [blk] to the correct cache and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block that is not currently
// being processed in consensus.
// assumes lock is held.
func (c *ChainState) addNonProcessingBlock(blk Block) (snowman.Block, error) {
	wrappedBlk := &BlockWrapper{
		Block: blk,
		state: c,
	}

	status, err := c.getStatus(blk)
	if err != nil {
		return nil, err
	}

	wrappedBlk.status = status
	blk.SetStatus(status)
	blkID := blk.ID()
	switch status {
	case choices.Accepted, choices.Rejected:
		c.decidedBlocks.Put(blkID, wrappedBlk)
	case choices.Processing:
		c.unverifiedBlocks.Put(blk.ID(), wrappedBlk)
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

// LastAcceptedBlockInternal returns the internal snowman.Block that was last last accepted
func (c *ChainState) LastAcceptedBlockInternal() snowman.Block {
	return c.LastAcceptedBlock().Block
}

// getStatus returns the status of [blk]. Assumes that [blk] is a known block.
// assumes lock is held.
func (c *ChainState) getStatus(blk snowman.Block) (choices.Status, error) {
	blkHeight := blk.Height()
	lastAcceptedHeight := c.lastAcceptedBlock.Height()
	if blkHeight > lastAcceptedHeight {
		return choices.Processing, nil
	}

	acceptedBlkID, err := c.getBlockIDAtHeight(blk.Height())
	if err != nil {
		return choices.Unknown, fmt.Errorf("failed to get acceptedID at height %d, below last accepted height: %w", blk.Height(), err)
	}

	if acceptedBlkID == blk.ID() {
		return choices.Accepted, nil
	}

	return choices.Rejected, nil
}

// heightKey ...
func heightKey(height uint64) []byte {
	heightKey := make([]byte, 8)
	binary.BigEndian.PutUint64(heightKey, height)
	return heightKey
}
