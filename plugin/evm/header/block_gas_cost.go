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

func BlockGasCost(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) uint64 {
	var step uint64 = ApricotPhase4BlockGasCostStep
	if config.IsApricotPhase5(timestamp) {
		step = ApricotPhase5BlockGasCostStep
	}
	return BlockGasCostWithStep(
		parent.BlockGasCost,
		step,
		parent.Time,
		timestamp,
	)
}

func BlockGasCostWithStep(
	parentCost *big.Int,
	step uint64,
	parentTime uint64,
	timestamp uint64,
) uint64 {
	// Handle AP3/AP4 boundary by returning the minimum value as the boundary.
	if parentCost == nil {
		return ap4.MinBlockGasCost
	}

	// Treat an invalid parent/current time combination as 0 elapsed time.
	var timeElapsed uint64
	if parentTime <= timestamp {
		timeElapsed = timestamp - parentTime
	}

	return ap4.BlockGasCost(
		parentCost.Uint64(),
		step,
		timeElapsed,
	)
}
