// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package miner

import (
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type configHooks struct {
	// allowDuplicateBlocks is a test only boolean
	// to allow mining of the same block multiple times.
	allowDuplicateBlocks bool
}

func (c *configHooks) SetTestOnlyAllowDuplicateBlocks() {
	c.allowDuplicateBlocks = true
}

func (c *configHooks) TestOnlyAllowDuplicateBlocks() bool {
	return c.allowDuplicateBlocks
}

func (miner *Miner) GenerateBlock(predicateContext *precompileconfig.PredicateContext) (*types.Block, error) {
	result := miner.worker.generateWork(&generateParams{
		timestamp:  miner.clock.Unix(),
		parentHash: miner.worker.chain.CurrentBlock().Hash(),
		coinbase:   miner.worker.etherbase(),
		beaconRoot: &common.Hash{},
		hooks: &generateParamsHooks{
			predicateContext: predicateContext,
		},
	})
	return result.block, result.err
}
