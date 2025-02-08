// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp176

import (
	"math"
	"math/big"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/holiman/uint256"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	MinTargetPerSecond  = 1_000_000                                 // P
	TargetConversion    = MaxTargetChangeRate * MaxTargetExcessDiff // D
	MaxTargetExcessDiff = 1 << 15                                   // Q
	MinBaseFee          = 1                                         // M

	TimeToFillCapacity            = 10   // in seconds
	TargetToMax                   = 2    // multiplier to convert from target per second to max per second
	TargetToPriceUpdateConversion = 43   // 43s ~= 30s * ln(2) which makes the price double at most every ~30 seconds
	MaxTargetChangeRate           = 1024 // Controls the rate that the target can change per block.
)

var minBaseFee = big.NewInt(MinBaseFee)

type State struct {
	Gas          gas.State
	TargetExcess gas.Gas
}

func (s *State) Target() gas.Gas {
	return gas.Gas(gas.CalculatePrice(
		MinTargetPerSecond,
		s.TargetExcess,
		TargetConversion,
	))
}

func (s *State) Capacity() gas.Gas {
	targetPerSecond := s.Target()
	maxCapacity, err := safemath.Mul(TimeToFillCapacity*TargetToMax, targetPerSecond)
	if err != nil {
		maxCapacity = math.MaxUint64
	}
	return maxCapacity
}

func (s *State) BaseFee() *big.Int {
	target := s.Target()
	priceUpdateConversion, err := safemath.Mul(TargetToPriceUpdateConversion, target)
	if err != nil {
		priceUpdateConversion = math.MaxUint64
	}

	bigExcess := new(big.Int).SetUint64(uint64(s.Gas.Excess))
	bigPriceUpdateFraction := new(big.Int).SetUint64(uint64(priceUpdateConversion))
	return fakeExponential(minBaseFee, bigExcess, bigPriceUpdateFraction)
}

func (s *State) AdvanceTime(seconds uint64) {
	targetPerSecond := s.Target()
	maxPerSecond, err := safemath.Mul(TargetToMax, targetPerSecond)
	if err != nil {
		maxPerSecond = math.MaxUint64
	}
	maxCapacity, err := safemath.Mul(TimeToFillCapacity, maxPerSecond)
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

func (s *State) ConsumeGas(
	gasUsed uint64,
	extraGasUsed *big.Int,
) error {
	var err error
	s.Gas, err = s.Gas.ConsumeGas(gas.Gas(gasUsed))
	if err != nil {
		return err
	}
	if extraGasUsed != nil {
		s.Gas, err = s.Gas.ConsumeGas(gas.Gas(extraGasUsed.Uint64()))
	}
	return err
}

func (s *State) UpdateTargetExcess(desiredTargetExcess gas.Gas) {
	previousTargetPerSecond := s.Target()
	s.TargetExcess = targetExcess(s.TargetExcess, desiredTargetExcess)
	newTargetPerSecond := s.Target()
	s.Gas.Excess = modifyExcess(
		s.Gas.Excess,
		newTargetPerSecond,
		previousTargetPerSecond,
	)
}

// DesiredTargetExcess approximates the desiredTargetExcess given the desired
// target.
//
// Namely, it returns D * ln(desiredTarget / P)
func DesiredTargetExcess(desiredTarget gas.Gas) gas.Gas {
	// desiredTarget = P * e^(desiredTargetExcess / D)
	desiredTargetMultiplier := float64(desiredTarget) / MinTargetPerSecond
	// desiredTargetMultiplier = e^(desiredTargetExcess / D)
	desiredTargetExponent := math.Log(desiredTargetMultiplier)
	// desiredTargetExponent = desiredTargetExcess / D
	desiredTargetExcess := TargetConversion * desiredTargetExponent
	return gas.Gas(desiredTargetExcess)
}

// fakeExponential approximates factor * e ** (numerator / denominator) using
// Taylor expansion.
func fakeExponential(factor, numerator, denominator *big.Int) *big.Int {
	var (
		output = new(big.Int)
		accum  = new(big.Int).Mul(factor, denominator)
	)
	for i := 1; accum.Sign() > 0; i++ {
		output.Add(output, accum)

		accum.Mul(accum, numerator)
		accum.Div(accum, denominator)
		accum.Div(accum, big.NewInt(int64(i)))
	}
	return output.Div(output, denominator)
}

// Implements calc_next_q from ACP-176
func targetExcess(excess, desired gas.Gas) gas.Gas {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxTargetExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// Returns x * T_{n+1} / T_n
//
// DO NOT MERGE: The spec should be updated to use target rather than K because
// K is always a multiple of FUpgradeTargetToPriceUpdateFraction
func modifyExcess(
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
