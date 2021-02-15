// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
)

// Block is the internal representation of a Block to be wrapped by BlockWrapper
type Block interface {
	snowman.Block
	// SetStatus sets the internal status of an existing block. This is used by ChainState
	// to allow internal blocks to maintain the most up to date status.
	SetStatus(choices.Status)
}

// BlockWrapper wraps a snowman Block and adds a smart caching layer
type BlockWrapper struct {
	Block
	status choices.Status

	state *ChainState
}

func (bw *BlockWrapper) Verify() error {
	bw.state.lock.Lock()
	defer bw.state.lock.Unlock()

	blkID := bw.ID()
	bw.state.unverifiedBlocks.Evict(blkID)

	bw.state.lock.Unlock()
	err := bw.Block.Verify()
	bw.state.lock.Lock()
	if err != nil {
		return err
	}
	bw.state.processingBlocks[blkID] = bw
	return nil
}

// Accept ...
func (bw *BlockWrapper) Accept() error {
	// TODO set flag on baseDB so that operations taking place within [bw]
	// are placed into the versiondb batch correctly.

	// We need to unlock state to call Accept, so we simply call Accept
	// before modifying state.
	if err := bw.Block.Accept(); err != nil {
		return err
	}

	bw.state.lock.Lock()
	defer bw.state.lock.Unlock()

	blkID := bw.ID()
	delete(bw.state.processingBlocks, blkID)
	bw.state.decidedBlocks.Put(blkID, bw)
	bw.status = choices.Accepted
	bw.state.lastAcceptedBlock = bw
	if err := bw.state.internalDB.Put(LastAcceptedKey, blkID[:]); err != nil {
		return err
	}

	if err := bw.state.acceptedBlocksByHeight.Put(heightKey(bw.Height()), blkID[:]); err != nil {
		return err
	}

	return bw.state.baseDB.Commit()
}

// Reject ...
func (bw *BlockWrapper) Reject() error {
	// Some implementations may require access to state, so we call Reject
	// before locking and modifying state.
	if err := bw.Block.Reject(); err != nil {
		return err
	}

	bw.state.lock.Lock()
	defer bw.state.lock.Unlock()

	delete(bw.state.processingBlocks, bw.ID())
	bw.state.decidedBlocks.Put(bw.ID(), bw)
	bw.status = choices.Rejected
	return bw.state.baseDB.Commit()
}

// Parent ...
func (bw *BlockWrapper) Parent() snowman.Block {
	parentID := bw.Block.Parent().ID()
	blk, err := bw.state.GetBlock(parentID)
	if err == nil {
		return blk
	}
	return &missing.Block{BlkID: parentID}
}

// Status ...
func (bw *BlockWrapper) Status() choices.Status {
	return bw.status
}
