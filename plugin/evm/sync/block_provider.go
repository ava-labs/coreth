// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

type blockProvider struct {
	chain *core.BlockChain
}

// TODO: this should be moved to a different place
func NewBlockProvider(chain *core.BlockChain) SummaryProvider {
	return &blockProvider{chain: chain}
}

// stateSummaryAtHeight returns the SyncSummary at [height] if valid and available.
func (bp *blockProvider) StateSummaryAtHeight(height uint64) (block.StateSummary, error) {
	blk := bp.chain.GetBlockByNumber(height)
	if blk == nil {
		return nil, fmt.Errorf("block not found for height (%d)", height)
	}

	if !bp.chain.HasState(blk.Root()) {
		return nil, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}

	summary, err := message.NewBlockSyncSummary(blk.Hash(), height, blk.Root())
	if err != nil {
		return nil, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}
	return summary, nil
}
