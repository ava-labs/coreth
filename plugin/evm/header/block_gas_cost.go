// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap4"
)

const (
	ApricotPhase4BlockGasCostStep = 50_000
	ApricotPhase5BlockGasCostStep = 200_000
)

func BlockGasCostFromHeader(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) uint64 {
	var step uint64 = ApricotPhase4BlockGasCostStep
	if config.IsApricotPhase5(timestamp) {
		step = ApricotPhase5BlockGasCostStep
	}
	// Treat an invalid parent/current time combination as 0 elapsed time.
	var timeElapsed uint64
	if parent.Time <= timestamp {
		timeElapsed = timestamp - parent.Time
	}
	return BlockGasCost(
		parent.BlockGasCost,
		step,
		timeElapsed,
	)
}

func BlockGasCost(
	parentCost *big.Int,
	step uint64,
	timeElapsed uint64,
) uint64 {
	// Handle AP3/AP4 boundary by returning the minimum value as the boundary.
	if parentCost == nil {
		return ap4.MinBlockGasCost
	}

	return ap4.BlockGasCost(
		parentCost.Uint64(),
		step,
		timeElapsed,
	)
}
