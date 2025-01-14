// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ethereum/go-ethereum/common"
)

var _ sync.SummaryProvider = &AtomicSyncProvider{}

type AtomicSyncProvider struct {
	chain      *core.BlockChain
	atomicTrie state.AtomicTrie
}

func NewAtomicProvider(chain *core.BlockChain, atomicTrie state.AtomicTrie) *AtomicSyncProvider {
	return &AtomicSyncProvider{chain: chain, atomicTrie: atomicTrie}
}

// StateSummaryAtHeight returns the block state summary  at [height] if valid and available.
func (a *AtomicSyncProvider) StateSummaryAtHeight(height uint64) (block.StateSummary, error) {
	atomicRoot, err := a.atomicTrie.Root(height)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve atomic trie root for height (%d): %w", height, err)
	}

	if atomicRoot == (common.Hash{}) {
		return nil, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	blk := a.chain.GetBlockByNumber(height)
	if blk == nil {
		return nil, fmt.Errorf("block not found for height (%d)", height)
	}

	if !a.chain.HasState(blk.Root()) {
		return nil, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}

	summary, err := NewAtomicSyncSummary(blk.Hash(), height, blk.Root(), atomicRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}
	return summary, nil
}
