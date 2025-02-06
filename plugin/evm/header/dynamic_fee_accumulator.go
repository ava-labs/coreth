// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	ErrDynamicFeeAccumulatorSize = wrappers.LongLen * 3

	FUpgradeMinTargetPerSecond          = 1_000_000
	FUpgradeTimeToFillCapacity          = 10      // in seconds
	FUpgradeTargetToPriceUpdateFraction = 43      // 43 ~= 30 * ln(2) which makes the price double at most every ~30 seconds
	FUpgradeMaxTargetRate               = 1024    // Controls the rate that the target can change per block.
	FUpgradeMaxTargetDiff               = 1 << 15 // Provides the block builder flexibility when changing the target
	FUpgradeTargetConversionFraction    = FUpgradeMaxTargetRate * FUpgradeMaxTargetDiff
)

var (
	ErrDynamicFeeAccumulatorInsufficientLength = errors.New("insufficient length for dynamic fee accumulator")

	fUpgradeMinBaseFee = common.Big1
)

type DynamicFeeAccumulator struct {
	GasState     gas.State
	TargetExcess gas.Gas
}

func ParseDynamicFeeAccumulator(bytes []byte) (DynamicFeeAccumulator, error) {
	if len(bytes) < ErrDynamicFeeAccumulatorSize {
		return DynamicFeeAccumulator{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeAccumulatorInsufficientLength,
			ErrDynamicFeeAccumulatorSize,
			len(bytes),
		)
	}

	return DynamicFeeAccumulator{
		GasState: gas.State{
			Capacity: gas.Gas(binary.BigEndian.Uint64(bytes)),
			Excess:   gas.Gas(binary.BigEndian.Uint64(bytes[wrappers.LongLen:])),
		},
		TargetExcess: gas.Gas(binary.BigEndian.Uint64(bytes[2*wrappers.LongLen:])),
	}, nil
}

func (d *DynamicFeeAccumulator) Target() gas.Gas {
	return gas.Gas(gas.CalculatePrice(
		FUpgradeMinTargetPerSecond,
		d.TargetExcess,
		FUpgradeTargetConversionFraction,
	))
}

func (d *DynamicFeeAccumulator) BaseFee() *big.Int {
	target := d.Target()
	priceUpdateFraction, err := safemath.Mul(FUpgradeTargetToPriceUpdateFraction, target)
	if err != nil {
		priceUpdateFraction = math.MaxUint64
	}

	bigExcess := new(big.Int).SetUint64(uint64(d.GasState.Excess))
	bigPriceUpdateFraction := new(big.Int).SetUint64(uint64(priceUpdateFraction))
	baseFee := fakeExponential(fUpgradeMinBaseFee, bigExcess, bigPriceUpdateFraction)
	return baseFee
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

func (d *DynamicFeeAccumulator) AdvanceTime(seconds uint64) {
	targetPerSecond := d.Target()
	maxPerSecond, err := safemath.Mul(2, targetPerSecond)
	if err != nil {
		maxPerSecond = math.MaxUint64
	}
	maxCapacity, err := safemath.Mul(FUpgradeTimeToFillCapacity, maxPerSecond)
	if err != nil {
		maxCapacity = math.MaxUint64
	}
	d.GasState = d.GasState.AdvanceTime(
		maxCapacity,
		maxPerSecond,
		targetPerSecond,
		seconds,
	)
}

func (d *DynamicFeeAccumulator) ConsumeGas(
	gasUsed uint64,
	extraGasUsed *big.Int,
) error {
	var err error
	d.GasState, err = d.GasState.ConsumeGas(gas.Gas(gasUsed))
	if err != nil {
		return err
	}
	if extraGasUsed != nil {
		d.GasState, err = d.GasState.ConsumeGas(gas.Gas(extraGasUsed.Uint64()))
	}
	return err
}

func (d *DynamicFeeAccumulator) UpdateTargetExcess(desiredTargetExcess gas.Gas) {
	previousTargetPerSecond := d.Target()
	d.TargetExcess = targetExcess(d.TargetExcess, desiredTargetExcess)
	newTargetPerSecond := d.Target()
	d.GasState.Excess = modifyExcess(
		d.GasState.Excess,
		newTargetPerSecond,
		previousTargetPerSecond,
	)
}

// Implements calc_next_q from ACP-176
func targetExcess(excess, desired gas.Gas) gas.Gas {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, FUpgradeMaxTargetDiff)
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

func (d *DynamicFeeAccumulator) Bytes() []byte {
	bytes := make([]byte, ErrDynamicFeeAccumulatorSize)
	binary.BigEndian.PutUint64(bytes, uint64(d.GasState.Capacity))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(d.GasState.Excess))
	binary.BigEndian.PutUint64(bytes[2*wrappers.LongLen:], uint64(d.TargetExcess))
	return bytes
}

// returns D * ln(desiredTarget / P)
func DesiredTargetExcess(desiredTarget gas.Gas) gas.Gas {
	return gas.Gas(FUpgradeTargetConversionFraction * math.Log(float64(desiredTarget)/FUpgradeMinTargetPerSecond))
}
