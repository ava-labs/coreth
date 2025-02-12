// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP4 implements the block gas cost logic activated by the Apricot Phase 4
// upgrade.
package ap4

import (
	"github.com/ethereum/go-ethereum/common/math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	MinBlockGasCost = 0
	MaxBlockGasCost = 1_000_000
	TargetBlockRate = 2 // in seconds

	// BlockGasCostStep is the rate at which the block gas cost changes per
	// second.
	//
	// This value as modified by the Apricot Phase 5 upgrade.
	BlockGasCostStep = 50_000
)

// BlockGasCost calculates the required block gas cost.
//
// cost = parentCost + step * (TargetBlockRate - timeElapsed)
//
// The returned cost is clamped to [MinBlockGasCost, MaxBlockGasCost].
func BlockGasCost(
	parentCost uint64,
	step uint64,
	timeElapsed uint64,
) uint64 {
	var (
		deviation   uint64
		op          func(uint64, uint64) (uint64, error)
		defaultCost uint64
	)
	if timeElapsed < TargetBlockRate {
		deviation = TargetBlockRate - timeElapsed
		op = safemath.Add
		defaultCost = MaxBlockGasCost
	} else {
		deviation = timeElapsed - TargetBlockRate
		op = safemath.Sub
		defaultCost = MinBlockGasCost
	}

	change, err := safemath.Mul(step, deviation)
	if err != nil {
		change = math.MaxUint64
	}
	cost, err := op(parentCost, change)
	if err != nil {
		cost = defaultCost
	}

	switch {
	case cost < MinBlockGasCost:
		return MinBlockGasCost
	case cost > MaxBlockGasCost:
		return MaxBlockGasCost
	default:
		return cost
	}
}
