// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package atomic

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ethereum/go-ethereum/common"
)

type atomicSyncProvider struct {
	chain      *core.BlockChain
	atomicTrie AtomicTrie
}

func NewAtomicProvider(chain *core.BlockChain, atomicTrie AtomicTrie) sync.SummaryProvider {
	return &atomicSyncProvider{chain: chain, atomicTrie: atomicTrie}
}

// StateSummaryAtHeight returns the SyncSummary at [height] if valid and available.
func (a *atomicSyncProvider) StateSummaryAtHeight(height uint64) (block.StateSummary, error) {
	atomicRoot, err := a.atomicTrie.Root(height)
	if err != nil {
		return nil, fmt.Errorf("error getting atomic trie root for height (%d): %w", height, err)
	}

	if (atomicRoot == common.Hash{}) {
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
