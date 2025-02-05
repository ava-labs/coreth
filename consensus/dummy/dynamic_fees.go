// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"

	customheader "github.com/ava-labs/coreth/plugin/evm/header"
)

func BigEqual(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}

func CalcGasLimit(config *params.ChainConfig, parent *types.Header, timestamp uint64) (uint64, error) {
	switch {
	case config.IsFUpgrade(timestamp):
		gasState, _, err := CalcACP176GasState(config, parent, timestamp)
		if err != nil {
			return 0, err
		}
		return uint64(gasState.GasState.Capacity), nil
	case config.IsCortina(timestamp):
		return params.CortinaGasLimit, nil
	case config.IsApricotPhase1(timestamp):
		return params.ApricotPhase1GasLimit, nil
	default:
		// The gas limit is set in phase1 to ApricotPhase1GasLimit because the
		// ceiling and floor were set to the same value such that the gas limit
		// converged to it. Since this is hardcoded now, we remove the ability
		// to configure it.
		return calcGasLimit(
			parent.GasUsed,
			parent.GasLimit,
			params.ApricotPhase1GasLimit,
			params.ApricotPhase1GasLimit,
		), nil
	}
}

// Copied from core.CalcGasLimit to avoid a circular dependency.
// DO NOT MERGE with this hack.
func calcGasLimit(parentGasUsed, parentGasLimit, gasFloor, gasCeil uint64) uint64 {
	// contrib = (parentGasUsed * 3 / 2) / 1024
	contrib := (parentGasUsed + parentGasUsed/2) / params.GasLimitBoundDivisor

	// decay = parentGasLimit / 1024 -1
	decay := parentGasLimit/params.GasLimitBoundDivisor - 1

	/*
		strategy: gasLimit of block-to-mine is set based on parent's
		gasUsed value.  if parentGasUsed > parentGasLimit * (2/3) then we
		increase it, otherwise lower it (or leave it unchanged if it's right
		at that usage) the amount increased/decreased depends on how far away
		from parentGasLimit * (2/3) parentGasUsed is.
	*/
	limit := parentGasLimit - decay + contrib
	if limit < params.MinGasLimit {
		limit = params.MinGasLimit
	}
	// If we're outside our allowed gas range, we try to hone towards them
	if limit < gasFloor {
		limit = parentGasLimit + decay
		if limit > gasFloor {
			limit = gasFloor
		}
	} else if limit > gasCeil {
		limit = parentGasLimit - decay
		if limit < gasCeil {
			limit = gasCeil
		}
	}
	return limit
}

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee as well as the encoding of the past
// pricing information for the child block.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	switch {
	case config.IsFUpgrade(timestamp):
		gasState, targetPerSecond, err := CalcACP176GasState(config, parent, timestamp)
		if err != nil {
			return nil, err
		}
		return CalcACP176BaseFee(gasState.GasState.Excess, targetPerSecond), nil
	case config.IsApricotPhase3(timestamp):
		feeWindow, err := CalcFeeWindow(config, parent, timestamp)
		if err != nil {
			return nil, err
		}
		return CalcFeeWindowBaseFee(config, parent, timestamp, feeWindow)
	default:
		return nil, nil
	}
}

func CalcHeaderExtra(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) ([]byte, error) {
	switch {
	case config.IsFUpgrade(timestamp):
		gasState, _, err := CalcACP176GasState(config, parent, timestamp)
		if err != nil {
			return nil, err
		}
		return gasState.Bytes(), nil
	case config.IsApricotPhase3(timestamp):
		feeWindow, err := CalcFeeWindow(config, parent, timestamp)
		if err != nil {
			return nil, err
		}
		return feeWindow.Bytes(), nil
	default:
		return nil, nil
	}
}

func VerifyHeaderGasFields(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit > params.MaxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	switch {
	case config.IsFUpgrade(header.Time):
		gasState, _, err := CalcACP176GasState(config, parent, header.Time)
		if err != nil {
			return err
		}
		if header.GasLimit != uint64(gasState.GasState.Capacity) {
			return fmt.Errorf("invalid gas limit: have %d, want %d", header.GasLimit, gasState.GasState.Capacity)
		}
	case config.IsCortina(header.Time):
		if header.GasLimit != params.CortinaGasLimit {
			return fmt.Errorf("expected gas limit to be %d in Cortina, but found %d", params.CortinaGasLimit, header.GasLimit)
		}
	case config.IsApricotPhase1(header.Time):
		if header.GasLimit != params.ApricotPhase1GasLimit {
			return fmt.Errorf("expected gas limit to be %d in ApricotPhase1, but found %d", params.ApricotPhase1GasLimit, header.GasLimit)
		}
	default:
		// Verify that the gas limit remains within allowed bounds
		diff := math.AbsDiff(parent.GasLimit, header.GasLimit)
		limit := parent.GasLimit / params.GasLimitBoundDivisor
		if diff >= limit || header.GasLimit < params.MinGasLimit {
			return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
		}
	}

	switch {
	case config.IsFUpgrade(header.Time):
		expectedGasState, _, err := CalcACP176GasState(config, parent, header.Time)
		if err != nil {
			return err
		}
		gasState, err := customheader.ParseDynamicFeeAccumulator(header.Extra)
		if err != nil {
			return err
		}
		if gasState.GasState != expectedGasState.GasState {
			return fmt.Errorf("invalid gas state: have %v, want %v", gasState.GasState, expectedGasState.GasState)
		}

		targetDiff := math.AbsDiff(gasState.TargetExcess, expectedGasState.TargetExcess)
		if targetDiff > FUpgradeMaxTargetDiff {
			return fmt.Errorf("invalid target excess: have %d, want %d += %d", gasState.TargetExcess, expectedGasState.TargetExcess, FUpgradeMaxTargetDiff)
		}
	case config.IsApricotPhase3(header.Time):
		feeWindow, err := CalcFeeWindow(config, parent, header.Time)
		if err != nil {
			return err
		}
		feeWindowBytes := feeWindow.Bytes()
		if !bytes.HasPrefix(header.Extra, feeWindowBytes) {
			return fmt.Errorf("expected header prefix: %x, found %x", feeWindowBytes, header.Extra)
		}
	}

	expectedBaseFee, err := CalcBaseFee(config, parent, header.Time)
	if err != nil {
		return fmt.Errorf("failed to calculate base fee: %w", err)
	}
	if !BigEqual(header.BaseFee, expectedBaseFee) {
		return fmt.Errorf("expected base fee: %d, found %d", expectedBaseFee, header.BaseFee)
	}

	// Verify BlockGasCost, ExtDataGasUsed not present before AP4
	if !config.IsApricotPhase4(header.Time) {
		if header.BlockGasCost != nil {
			return fmt.Errorf("invalid blockGasCost before fork: have %d, want <nil>", header.BlockGasCost)
		}
		if header.ExtDataGasUsed != nil {
			return fmt.Errorf("invalid extDataGasUsed before fork: have %d, want <nil>", header.ExtDataGasUsed)
		}
		return nil
	}

	// Enforce BlockGasCost constraints
	blockGasCostStep := ApricotPhase4BlockGasCostStep
	if config.IsApricotPhase5(header.Time) {
		blockGasCostStep = ApricotPhase5BlockGasCostStep
	}
	expectedBlockGasCost := calcBlockGasCost(
		ApricotPhase4TargetBlockRate,
		ApricotPhase4MinBlockGasCost,
		ApricotPhase4MaxBlockGasCost,
		blockGasCostStep,
		parent.BlockGasCost,
		parent.Time, header.Time,
	)
	if header.BlockGasCost == nil {
		return errBlockGasCostNil
	}
	if !header.BlockGasCost.IsUint64() {
		return errBlockGasCostTooLarge
	}
	if header.BlockGasCost.Cmp(expectedBlockGasCost) != 0 {
		return fmt.Errorf("invalid block gas cost: have %d, want %d", header.BlockGasCost, expectedBlockGasCost)
	}
	// ExtDataGasUsed correctness is checked during block validation
	// (when the validator has access to the block contents)
	if header.ExtDataGasUsed == nil {
		return errExtDataGasUsedNil
	}
	if !header.ExtDataGasUsed.IsUint64() {
		return errExtDataGasUsedTooLarge
	}
	return nil
}

// EstimateNextBaseFee attempts to estimate the next base fee based on a block with [parent] being built at
// [timestamp].
// If [timestamp] is less than the timestamp of [parent], then it uses the same timestamp as parent.
// Warning: This function should only be used in estimation and should not be used when calculating the canonical
// base fee for a subsequent block.
func EstimateNextBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	if timestamp < parent.Time {
		timestamp = parent.Time
	}
	return CalcBaseFee(config, parent, timestamp)
}

// MinRequiredTip is the estimated minimum tip a transaction would have
// needed to pay to be included in a given block (assuming it paid a tip
// proportional to its gas usage). In reality, there is no minimum tip that
// is enforced by the consensus engine and high tip paying transactions can
// subsidize the inclusion of low tip paying transactions. The only
// correctness check performed is that the sum of all tips is >= the
// required block fee.
//
// This function will return nil for all return values prior to Apricot Phase 4.
func MinRequiredTip(config *params.ChainConfig, header *types.Header) (*big.Int, error) {
	if !config.IsApricotPhase4(header.Time) {
		return nil, nil
	}
	if header.BaseFee == nil {
		return nil, errBaseFeeNil
	}
	if header.BlockGasCost == nil {
		return nil, errBlockGasCostNil
	}
	if header.ExtDataGasUsed == nil {
		return nil, errExtDataGasUsedNil
	}

	// minTip = requiredBlockFee/blockGasUsage
	requiredBlockFee := new(big.Int).Mul(
		header.BlockGasCost,
		header.BaseFee,
	)
	blockGasUsage := new(big.Int).Add(
		new(big.Int).SetUint64(header.GasUsed),
		header.ExtDataGasUsed,
	)
	return new(big.Int).Div(requiredBlockFee, blockGasUsage), nil
}
