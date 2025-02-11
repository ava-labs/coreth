// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap3"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

var (
	Big2Pow256    = new(big.Int).Lsh(common.Big1, 256)
	BigMaxUint256 = new(big.Int).Sub(Big2Pow256, common.Big1)

	ApricotPhase3MinBaseFee     = big.NewInt(params.ApricotPhase3MinBaseFee)
	ApricotPhase3MaxBaseFee     = big.NewInt(params.ApricotPhase3MaxBaseFee)
	ApricotPhase4MinBaseFee     = big.NewInt(params.ApricotPhase4MinBaseFee)
	ApricotPhase4MaxBaseFee     = big.NewInt(params.ApricotPhase4MaxBaseFee)
	ApricotPhase3InitialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
	EtnaMinBaseFee              = big.NewInt(params.EtnaMinBaseFee)

	ApricotPhase4BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase4BaseFeeChangeDenominator)
	ApricotPhase5BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase5BaseFeeChangeDenominator)

	ApricotPhase3BlockGasFee      uint64 = 1_000_000
	ApricotPhase4MinBlockGasCost         = new(big.Int).Set(common.Big0)
	ApricotPhase4MaxBlockGasCost         = big.NewInt(1_000_000)
	ApricotPhase4BlockGasCostStep        = big.NewInt(50_000)
	ApricotPhase4TargetBlockRate  uint64 = 2 // in seconds
	ApricotPhase5BlockGasCostStep        = big.NewInt(200_000)
)

func CalcFeeWindow(config *params.ChainConfig, parent *types.Header, timestamp uint64) (ap3.Window, error) {
	if timestamp < parent.Time {
		return ap3.Window{}, fmt.Errorf("cannot calculate fee window for timestamp %d prior to parent timestamp %d", timestamp, parent.Time)
	}

	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	rules := config.GetAvalancheRules(parent.Time)
	if !rules.IsApricotPhase3 || parent.Number.Cmp(common.Big0) == 0 {
		return ap3.Window{}, nil
	}

	dynamicFeeWindow, err := header.ParseDynamicFeeWindow(parent.Extra)
	if err != nil {
		return ap3.Window{}, err
	}

	// Add in parent's consumed gas
	var blockGasCost, parentExtraStateGasUsed uint64
	switch {
	case rules.IsApricotPhase5:
		// blockGasCost has been removed in AP5, so it is left as 0.

		// At the start of a new network, the parent
		// may not have a populated ExtDataGasUsed.
		if parent.ExtDataGasUsed != nil {
			parentExtraStateGasUsed = parent.ExtDataGasUsed.Uint64()
		}
	case rules.IsApricotPhase4:
		// The blockGasCost is paid by the effective tips in the block using
		// the block's value of baseFee.
		blockGasCost = calcBlockGasCost(
			ApricotPhase4TargetBlockRate,
			ApricotPhase4MinBlockGasCost,
			ApricotPhase4MaxBlockGasCost,
			ApricotPhase4BlockGasCostStep,
			parent.BlockGasCost,
			parent.Time, timestamp,
		).Uint64()

		// On the boundary of AP3 and AP4 or at the start of a new network, the
		// parent may not have a populated ExtDataGasUsed.
		if parent.ExtDataGasUsed != nil {
			parentExtraStateGasUsed = parent.ExtDataGasUsed.Uint64()
		}
	default:
		blockGasCost = ApricotPhase3BlockGasFee
	}

	// Compute the new state of the gas rolling window.
	dynamicFeeWindow.Add(parent.GasUsed, parentExtraStateGasUsed, blockGasCost)

	// roll the window over by the difference between the timestamps to generate
	// the new rollup window.
	dynamicFeeWindow.Shift(timestamp - parent.Time)
	return dynamicFeeWindow, nil
}

// calcBlockGasCost calculates the required block gas cost. If [parentTime]
// > [currentTime], the timeElapsed will be treated as 0.
func calcBlockGasCost(
	targetBlockRate uint64,
	minBlockGasCost *big.Int,
	maxBlockGasCost *big.Int,
	blockGasCostStep *big.Int,
	parentBlockGasCost *big.Int,
	parentTime, currentTime uint64,
) *big.Int {
	// Handle AP3/AP4 boundary by returning the minimum value as the boundary.
	if parentBlockGasCost == nil {
		return new(big.Int).Set(minBlockGasCost)
	}

	// Treat an invalid parent/current time combination as 0 elapsed time.
	var timeElapsed uint64
	if parentTime <= currentTime {
		timeElapsed = currentTime - parentTime
	}

	var blockGasCost *big.Int
	if timeElapsed < targetBlockRate {
		blockGasCostDelta := new(big.Int).Mul(blockGasCostStep, new(big.Int).SetUint64(targetBlockRate-timeElapsed))
		blockGasCost = new(big.Int).Add(parentBlockGasCost, blockGasCostDelta)
	} else {
		blockGasCostDelta := new(big.Int).Mul(blockGasCostStep, new(big.Int).SetUint64(timeElapsed-targetBlockRate))
		blockGasCost = new(big.Int).Sub(parentBlockGasCost, blockGasCostDelta)
	}

	blockGasCost = selectBigWithinBounds(minBlockGasCost, blockGasCost, maxBlockGasCost)
	if !blockGasCost.IsUint64() {
		blockGasCost = new(big.Int).SetUint64(math.MaxUint64)
	}
	return blockGasCost
}

func CalcFeeWindowBaseFee(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
	window ap3.Window,
) (*big.Int, error) {
	if timestamp < parent.Time {
		return nil, fmt.Errorf("cannot calculate base fee for timestamp %d prior to parent timestamp %d", timestamp, parent.Time)
	}

	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	rules := config.GetAvalancheRules(parent.Time)
	if !rules.IsApricotPhase3 || parent.Number.Cmp(common.Big0) == 0 {
		return big.NewInt(params.ApricotPhase3InitialBaseFee), nil
	}

	// If AP5, use a less responsive BaseFeeChangeDenominator and a higher gas
	// block limit
	var (
		baseFee                  = new(big.Int).Set(parent.BaseFee)
		baseFeeChangeDenominator = ApricotPhase4BaseFeeChangeDenominator
		parentGasTarget          = params.ApricotPhase3TargetGas
	)
	if rules.IsApricotPhase5 {
		baseFeeChangeDenominator = ApricotPhase5BaseFeeChangeDenominator
		parentGasTarget = params.ApricotPhase5TargetGas
	}
	parentGasTargetBig := new(big.Int).SetUint64(parentGasTarget)

	// Calculate the amount of gas consumed within the rollup window.
	totalGas := window.Sum()
	if totalGas == parentGasTarget {
		return baseFee, nil
	}

	num := new(big.Int)

	if totalGas > parentGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		num.SetUint64(totalGas - parentGasTarget)
		num.Mul(num, parent.BaseFee)
		num.Div(num, parentGasTargetBig)
		num.Div(num, baseFeeChangeDenominator)
		baseFeeDelta := math.BigMax(num, common.Big1)

		baseFee.Add(baseFee, baseFeeDelta)
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		num.SetUint64(parentGasTarget - totalGas)
		num.Mul(num, parent.BaseFee)
		num.Div(num, parentGasTargetBig)
		num.Div(num, baseFeeChangeDenominator)
		baseFeeDelta := math.BigMax(num, common.Big1)
		// If [roll] is greater than [rollupWindow], apply the state transition to the base fee to account
		// for the interval during which no blocks were produced.
		// We use roll/rollupWindow, so that the transition is applied for every [rollupWindow] seconds
		// that has elapsed between the parent and this block.
		roll := timestamp - parent.Time
		if roll > ap3.WindowLen {
			// Note: roll/rollupWindow must be greater than 1 since we've checked that roll > rollupWindow
			baseFeeDelta = new(big.Int).Mul(baseFeeDelta, new(big.Int).SetUint64(roll/ap3.WindowLen))
		}
		baseFee.Sub(baseFee, baseFeeDelta)
	}

	// Ensure that the base fee does not increase/decrease outside of the bounds
	switch {
	case rules.IsEtna:
		baseFee = selectBigWithinBounds(EtnaMinBaseFee, baseFee, BigMaxUint256)
	case rules.IsApricotPhase5:
		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, BigMaxUint256)
	case rules.IsApricotPhase4:
		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, ApricotPhase4MaxBaseFee)
	default:
		baseFee = selectBigWithinBounds(ApricotPhase3MinBaseFee, baseFee, ApricotPhase3MaxBaseFee)
	}
	return baseFee, nil
}

// selectBigWithinBounds returns [value] if it is within the bounds:
// lowerBound <= value <= upperBound or the bound at either end if [value]
// is outside of the defined boundaries.
func selectBigWithinBounds(lowerBound, value, upperBound *big.Int) *big.Int {
	switch {
	case lowerBound != nil && value.Cmp(lowerBound) < 0:
		return new(big.Int).Set(lowerBound)
	case upperBound != nil && value.Cmp(upperBound) > 0:
		return new(big.Int).Set(upperBound)
	default:
		return value
	}
}
