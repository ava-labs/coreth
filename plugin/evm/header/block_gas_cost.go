// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap4"
	"github.com/ava-labs/coreth/plugin/evm/ap5"
)

var (
	errBaseFeeNil        = errors.New("base fee is nil")
	errBlockGasCostNil   = errors.New("block gas cost is nil")
	errExtDataGasUsedNil = errors.New("extDataGasUsed is nil")
)

// BlockGasCost calculates the required block gas cost based on the parent
// header and the timestamp of the new block.
func BlockGasCost(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) uint64 {
	step := uint64(ap4.BlockGasCostStep)
	if config.IsApricotPhase5(timestamp) {
		step = ap5.BlockGasCostStep
	}
	// Treat an invalid parent/current time combination as 0 elapsed time.
	//
	// TODO: Does it even make sense to handle this? The timestamp should be
	// verified to ensure this never happens.
	var timeElapsed uint64
	if parent.Time <= timestamp {
		timeElapsed = timestamp - parent.Time
	}
	return BlockGasCostWithStep(
		parent.BlockGasCost,
		step,
		timeElapsed,
	)
}

// BlockGasCostWithStep calculates the required block gas cost based on the
// parent cost and the time difference between the parent block and new block.
//
// This is a helper function that allows the caller to manually specify the step
// value to use.
func BlockGasCostWithStep(
	parentCost *big.Int,
	step uint64,
	timeElapsed uint64,
) uint64 {
	// Handle AP3/AP4 boundary by returning the minimum value as the boundary.
	if parentCost == nil {
		return ap4.MinBlockGasCost
	}

	// [ap4.MaxBlockGasCost] is <= MaxUint64, so we know that parentCost is
	// always going to be a valid uint64.
	return ap4.BlockGasCost(
		parentCost.Uint64(),
		step,
		timeElapsed,
	)
}

// EstimateRequiredTip is the estimated tip a transaction would have needed to
// pay to be included in a given block (assuming it paid a tip proportional to
// its gas usage).
//
// In reality, there is no minimum tip that is enforced by the consensus engine
// and high tip paying transactions can subsidize the inclusion of low tip
// paying transactions. The only correctness check performed is that the sum of
// all tips is >= the required block fee.
//
// This function will return nil for all return values prior to Apricot Phase 4.
func EstimateRequiredTip(
	config *params.ChainConfig,
	header *types.Header,
) (*big.Int, error) {
	switch {
	case !config.IsApricotPhase4(header.Time):
		return nil, nil
	case header.BaseFee == nil:
		return nil, errBaseFeeNil
	case header.ExtDataGasUsed == nil:
		return nil, errExtDataGasUsedNil
	case header.BlockGasCost == nil:
		return nil, errBlockGasCostNil
	}

	// totalGasUsed = GasUsed + ExtDataGasUsed
	totalGasUsed := new(big.Int).SetUint64(header.GasUsed)
	totalGasUsed.Add(totalGasUsed, header.ExtDataGasUsed)

	// requiredTip = (blockGasCost * baseFee) / totalGasUsed
	requiredTip := new(big.Int)
	requiredTip.Mul(header.BlockGasCost, header.BaseFee) // Total required fee
	requiredTip.Div(requiredTip, totalGasUsed)
	return requiredTip, nil
}
