// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP176 implements the fee logic specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md
package acp176

import (
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/holiman/uint256"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	MinTargetPerSecond  = 1_000_000                                 // P
	TargetConversion    = MaxTargetChangeRate * MaxTargetExcessDiff // D
	MaxTargetExcessDiff = 1 << 15                                   // Q
	MinGasPrice         = 1                                         // M

	TimeToFillCapacity            = 10   // in seconds
	TargetToMax                   = 2    // multiplier to convert from target per second to max per second
	TargetToPriceUpdateConversion = 43   // 43s ~= 30s * ln(2) which makes the price double at most every ~30 seconds
	MaxTargetChangeRate           = 1024 // Controls the rate that the target can change per block.

	targetToMaxCapacity = TargetToMax * TimeToFillCapacity
	maxTargetExcess     = 1_024_950_627 // TargetConversion * ln(MaxUint64 / MinTargetPerSecond) + 1
)

// State represents the current state of the gas pricing and constraints.
type State struct {
	Gas          gas.State
	TargetExcess gas.Gas // q
}

// Target returns the target gas consumed per second, `T`.
//
// Target = MinTargetPerSecond * e^(TargetExcess / TargetConversion)
func (s *State) Target() gas.Gas {
	return gas.Gas(gas.CalculatePrice(
		MinTargetPerSecond,
		s.TargetExcess,
		TargetConversion,
	))
}

// MaxCapacity returns the maximum possible accrued gas capacity, `C`.
func (s *State) MaxCapacity() gas.Gas {
	targetPerSecond := s.Target()
	maxCapacity, err := safemath.Mul(targetToMaxCapacity, targetPerSecond)
	if err != nil {
		maxCapacity = math.MaxUint64
	}
	return maxCapacity
}

// GasPrice returns the current required fee per gas.
//
// GasPrice = MinGasPrice * e^(Excess / (Target() * TargetToPriceUpdateConversion))
func (s *State) GasPrice() gas.Price {
	target := s.Target()
	priceUpdateConversion, err := safemath.Mul(TargetToPriceUpdateConversion, target) // K
	if err != nil {
		priceUpdateConversion = math.MaxUint64
	}

	return gas.CalculatePrice(MinGasPrice, s.Gas.Excess, priceUpdateConversion)
}

// AdvanceTime increases the gas capacity and decreases the gas excess based on
// the elapsed seconds.
func (s *State) AdvanceTime(seconds uint64) {
	targetPerSecond := s.Target()
	maxPerSecond, err := safemath.Mul(TargetToMax, targetPerSecond) // R
	if err != nil {
		maxPerSecond = math.MaxUint64
	}
	maxCapacity, err := safemath.Mul(TimeToFillCapacity, maxPerSecond) // C
	if err != nil {
		maxCapacity = math.MaxUint64
	}
	s.Gas = s.Gas.AdvanceTime(
		maxCapacity,
		maxPerSecond,
		targetPerSecond,
		seconds,
	)
}

// ConsumeGas decreases the gas capacity and increases the gas excess by
// gasUsed + extraGasUsed. If the gas capacity is insufficient, an error is
// returned.
func (s *State) ConsumeGas(
	gasUsed uint64,
	extraGasUsed *big.Int,
) error {
	newGas, err := s.Gas.ConsumeGas(gas.Gas(gasUsed))
	if err != nil {
		return err
	}

	if extraGasUsed == nil {
		s.Gas = newGas
		return nil
	}
	if !extraGasUsed.IsUint64() {
		return fmt.Errorf("%w: extraGasUsed (%d) exceeds MaxUint64",
			gas.ErrInsufficientCapacity,
			extraGasUsed,
		)
	}
	newGas, err = newGas.ConsumeGas(gas.Gas(extraGasUsed.Uint64()))
	if err != nil {
		return err
	}

	s.Gas = newGas
	return nil
}

// UpdateTargetExcess updates the targetExcess to be as close as possible to the
// desiredTargetExcess without exceeding the maximum targetExcess change.
func (s *State) UpdateTargetExcess(desiredTargetExcess gas.Gas) {
	previousTargetPerSecond := s.Target()
	s.TargetExcess = targetExcess(s.TargetExcess, desiredTargetExcess)
	newTargetPerSecond := s.Target()
	s.Gas.Excess = scaleExcess(
		s.Gas.Excess,
		newTargetPerSecond,
		previousTargetPerSecond,
	)
}

// DesiredTargetExcess calculates the optimal desiredTargetExcess given the
// desired target.
//
// This could be solved directly by calculating D * ln(desiredTarget / P) using
// floating point math. However, it introduces inaccuracies. So, we use a binary
// search to find the closest integer solution.
func DesiredTargetExcess(desiredTarget gas.Gas) gas.Gas {
	return gas.Gas(sort.Search(maxTargetExcess, func(targetExcessGuess int) bool {
		state := State{
			TargetExcess: gas.Gas(targetExcessGuess),
		}
		return state.Target() >= desiredTarget
	}))
}

// targetExcess calculates the optimal new targetExcess for a block proposer to
// include given the current and desired excess values.
func targetExcess(excess, desired gas.Gas) gas.Gas {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxTargetExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// scaleExcess scales the excess during gas target modifications to keep the
// price constant.
func scaleExcess(
	excess,
	newTargetPerSecond,
	previousTargetPerSecond gas.Gas,
) gas.Gas {
	var bigExcess uint256.Int
	bigExcess.SetUint64(uint64(excess))

	var bigTarget uint256.Int
	bigTarget.SetUint64(uint64(newTargetPerSecond))
	bigExcess.Mul(&bigExcess, &bigTarget)

	bigTarget.SetUint64(uint64(previousTargetPerSecond))
	bigExcess.Div(&bigExcess, &bigTarget)
	return gas.Gas(bigExcess.Uint64())
}
