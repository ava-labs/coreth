// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"fmt"
	"math"
	"math/big"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ethereum/go-ethereum/common"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	FUpgradeMinTargetPerSecond          = 1_000_000
	FUpgradeTimeToFillCapacity          = 10        // in seconds
	FUpgradeTargetToPriceUpdateFraction = 43        // 43 ~= 30 * ln(2) which makes the price double at most every ~30 seconds
	FUpgradeMaxTargetDiff               = 1_024     // 2^10
	FUpgradeTargetConversionFraction    = 1_048_576 // 2^20
)

var fUpgradeMinBaseFee = common.Big1

func CalcACP176GasState(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (
	header.DynamicFeeAccumulator,
	gas.Gas, // targetPerSecond
	error,
) {
	if timestamp < parent.Time {
		return header.DynamicFeeAccumulator{}, 0, fmt.Errorf("cannot calculate gas state for timestamp %d prior to parent timestamp %d",
			timestamp,
			parent.Time,
		)
	}

	var gasAccumulator header.DynamicFeeAccumulator
	if config.IsFUpgrade(parent.Time) && parent.Number.Cmp(common.Big0) != 0 {
		// If the parent block was running with ACP-176, we need to take the
		// prior gas state and consume the gas as expected.
		var err error
		gasAccumulator, err = header.ParseDynamicFeeAccumulator(parent.Extra)
		if err != nil {
			return header.DynamicFeeAccumulator{}, 0, err
		}

		gasAccumulator.GasState, err = gasAccumulator.GasState.ConsumeGas(gas.Gas(parent.GasUsed))
		if err != nil {
			return header.DynamicFeeAccumulator{}, 0, err
		}

		if parent.ExtDataGasUsed != nil {
			gasAccumulator.GasState, err = gasAccumulator.GasState.ConsumeGas(gas.Gas(parent.ExtDataGasUsed.Uint64()))
			if err != nil {
				return header.DynamicFeeAccumulator{}, 0, err
			}
		}
	}

	targetPerSecond := gas.Gas(gas.CalculatePrice(
		FUpgradeMinTargetPerSecond,
		gasAccumulator.TargetExcess,
		FUpgradeTargetConversionFraction,
	))
	maxPerSecond, err := safemath.Mul(2, targetPerSecond)
	if err != nil {
		maxPerSecond = math.MaxUint64
	}
	maxCapacity, err := safemath.Mul(FUpgradeTimeToFillCapacity, maxPerSecond)
	if err != nil {
		maxCapacity = math.MaxUint64
	}

	gasAccumulator.GasState = gasAccumulator.GasState.AdvanceTime(
		maxCapacity,
		maxPerSecond,
		targetPerSecond,
		timestamp-parent.Time,
	)
	return gasAccumulator, targetPerSecond, nil
}

func CalcACP176BaseFee(excess, targetPerSecond gas.Gas) *big.Int {
	priceUpdateFraction, err := safemath.Mul(FUpgradeTargetToPriceUpdateFraction, targetPerSecond)
	if err != nil {
		priceUpdateFraction = math.MaxUint64
	}

	bigExcess := new(big.Int).SetUint64(uint64(excess))
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
