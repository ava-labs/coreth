// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap3"
	"github.com/ethereum/go-ethereum/common"
)

const (
	DynamicFeeWindowSize = wrappers.LongLen * ap3.WindowLen

	ApricotPhase3IntrinsicBlockGas = 1_000_000
)

var ErrDynamicFeeWindowInsufficientLength = errors.New("insufficient length for dynamic fee window")

func CalculateDynamicFeeWindow(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (ap3.Window, error) {
	if timestamp < parent.Time {
		return ap3.Window{}, fmt.Errorf("cannot calculate fee window for timestamp %d prior to parent timestamp %d",
			timestamp,
			parent.Time,
		)
	}

	// If the current block is the first AP3 block, or it is the genesis block
	// return the initial window.
	rules := config.GetAvalancheRules(parent.Time)
	if !rules.IsApricotPhase3 || parent.Number.Cmp(common.Big0) == 0 {
		return ap3.Window{}, nil
	}

	dynamicFeeWindow, err := ParseDynamicFeeWindow(parent.Extra)
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
		blockGasCost = BlockGasCostWithStep(
			parent.BlockGasCost,
			ApricotPhase4BlockGasCostStep,
			parent.Time,
			timestamp,
		)

		// On the boundary of AP3 and AP4 or at the start of a new network, the
		// parent may not have a populated ExtDataGasUsed.
		if parent.ExtDataGasUsed != nil {
			parentExtraStateGasUsed = parent.ExtDataGasUsed.Uint64()
		}
	default:
		blockGasCost = ApricotPhase3IntrinsicBlockGas
	}

	// Compute the new state of the gas rolling window.
	dynamicFeeWindow.Add(parent.GasUsed, parentExtraStateGasUsed, blockGasCost)

	// roll the window over by the difference between the timestamps to generate
	// the new rollup window.
	dynamicFeeWindow.Shift(timestamp - parent.Time)
	return dynamicFeeWindow, nil
}

// func CalcFeeWindowBaseFee(
// 	config *params.ChainConfig,
// 	parent *types.Header,
// 	timestamp uint64,
// 	window ap3.Window,
// ) (*big.Int, error) {
// 	if timestamp < parent.Time {
// 		return nil, fmt.Errorf("cannot calculate base fee for timestamp %d prior to parent timestamp %d",
// 			timestamp,
// 			parent.Time,
// 		)
// 	}

// 	// If the current block is the first EIP-1559 block, or it is the genesis block
// 	// return the initial slice and initial base fee.
// 	rules := config.GetAvalancheRules(parent.Time)
// 	if !rules.IsApricotPhase3 || parent.Number.Cmp(common.Big0) == 0 {
// 		return big.NewInt(params.ApricotPhase3InitialBaseFee), nil
// 	}

// 	// If AP5, use a less responsive BaseFeeChangeDenominator and a higher gas
// 	// block limit
// 	var (
// 		baseFee                  = new(big.Int).Set(parent.BaseFee)
// 		baseFeeChangeDenominator = ApricotPhase4BaseFeeChangeDenominator
// 		parentGasTarget          = params.ApricotPhase3TargetGas
// 	)
// 	if rules.IsApricotPhase5 {
// 		baseFeeChangeDenominator = ApricotPhase5BaseFeeChangeDenominator
// 		parentGasTarget = params.ApricotPhase5TargetGas
// 	}
// 	parentGasTargetBig := new(big.Int).SetUint64(parentGasTarget)

// 	// Calculate the amount of gas consumed within the rollup window.
// 	totalGas := window.Sum()
// 	if totalGas == parentGasTarget {
// 		return baseFee, nil
// 	}

// 	num := new(big.Int)

// 	if totalGas > parentGasTarget {
// 		// If the parent block used more gas than its target, the baseFee should increase.
// 		num.SetUint64(totalGas - parentGasTarget)
// 		num.Mul(num, parent.BaseFee)
// 		num.Div(num, parentGasTargetBig)
// 		num.Div(num, baseFeeChangeDenominator)
// 		baseFeeDelta := math.BigMax(num, common.Big1)

// 		baseFee.Add(baseFee, baseFeeDelta)
// 	} else {
// 		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
// 		num.SetUint64(parentGasTarget - totalGas)
// 		num.Mul(num, parent.BaseFee)
// 		num.Div(num, parentGasTargetBig)
// 		num.Div(num, baseFeeChangeDenominator)
// 		baseFeeDelta := math.BigMax(num, common.Big1)
// 		// If [roll] is greater than [rollupWindow], apply the state transition to the base fee to account
// 		// for the interval during which no blocks were produced.
// 		// We use roll/rollupWindow, so that the transition is applied for every [rollupWindow] seconds
// 		// that has elapsed between the parent and this block.
// 		roll := timestamp - parent.Time
// 		if roll > ap3.WindowLen {
// 			// Note: roll/rollupWindow must be greater than 1 since we've checked that roll > rollupWindow
// 			baseFeeDelta = new(big.Int).Mul(baseFeeDelta, new(big.Int).SetUint64(roll/ap3.WindowLen))
// 		}
// 		baseFee.Sub(baseFee, baseFeeDelta)
// 	}

// 	// Ensure that the base fee does not increase/decrease outside of the bounds
// 	switch {
// 	case rules.IsEtna:
// 		baseFee = selectBigWithinBounds(EtnaMinBaseFee, baseFee, BigMaxUint256)
// 	case rules.IsApricotPhase5:
// 		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, BigMaxUint256)
// 	case rules.IsApricotPhase4:
// 		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, ApricotPhase4MaxBaseFee)
// 	default:
// 		baseFee = selectBigWithinBounds(ApricotPhase3MinBaseFee, baseFee, ApricotPhase3MaxBaseFee)
// 	}
// 	return baseFee, nil
// }

// // selectBigWithinBounds returns [value] if it is within the bounds:
// // lowerBound <= value <= upperBound or the bound at either end if [value]
// // is outside of the defined boundaries.
// func selectBigWithinBounds(lowerBound, value, upperBound *big.Int) *big.Int {
// 	switch {
// 	case lowerBound != nil && value.Cmp(lowerBound) < 0:
// 		return new(big.Int).Set(lowerBound)
// 	case upperBound != nil && value.Cmp(upperBound) > 0:
// 		return new(big.Int).Set(upperBound)
// 	default:
// 		return value
// 	}
// }

func ParseDynamicFeeWindow(bytes []byte) (ap3.Window, error) {
	if len(bytes) < DynamicFeeWindowSize {
		return ap3.Window{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeWindowInsufficientLength,
			DynamicFeeWindowSize,
			len(bytes),
		)
	}

	var window ap3.Window
	for i := range window {
		offset := i * wrappers.LongLen
		window[i] = binary.BigEndian.Uint64(bytes[offset:])
	}
	return window, nil
}

func DynamicFeeWindowBytes(w ap3.Window) []byte {
	bytes := make([]byte, DynamicFeeWindowSize)
	for i, v := range w {
		offset := i * wrappers.LongLen
		binary.BigEndian.PutUint64(bytes[offset:], v)
	}
	return bytes
}
