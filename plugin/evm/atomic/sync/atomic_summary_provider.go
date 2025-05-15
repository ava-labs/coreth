// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/plugin/evm/sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

var _ sync.SummaryProvider = (*atomicSummaryProvider)(nil)

type atomicSummaryProvider struct {
	atomicTrie AtomicTrie
}

func NewAtomicSyncSummaryProvider(atomicTrie AtomicTrie) *atomicSummaryProvider {
	return &atomicSummaryProvider{
		atomicTrie: atomicTrie,
	}
}

// StateSummaryAtBlock returns the block state summary at [blk] if valid.
func (a *atomicSummaryProvider) StateSummaryAtBlock(blk *types.Block) (block.StateSummary, error) {
	height := blk.NumberU64()
	atomicRoot, err := a.atomicTrie.Root(height)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve atomic trie root for height (%d): %w", height, err)
	}

	if atomicRoot == (common.Hash{}) {
		return nil, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	summary, err := NewAtomicSyncSummary(blk.Hash(), height, blk.Root(), atomicRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}
	return summary, nil
}
