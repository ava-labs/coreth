// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

// GasLimit takes the previous header and the timestamp of its child block and
// calculates the gas limit for the child block.
func GasLimit(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (uint64, error) {
	switch {
	case config.IsFUpgrade(timestamp):
		gasState, err := CalculateDynamicFeeAccumulator(config, parent, timestamp)
		if err != nil {
			return 0, err
		}
		return uint64(gasState.Gas.Capacity), nil
	case config.IsCortina(timestamp):
		return params.CortinaGasLimit, nil
	case config.IsApricotPhase1(timestamp):
		return params.ApricotPhase1GasLimit, nil
	default:
		// The gas limit is set in phase1 to ApricotPhase1GasLimit because the
		// ceiling and floor were set to the same value such that the gas limit
		// converged to it. Since this is hardcoded now, we remove the ability
		// to configure it.
		return gasLimit(
			parent.GasUsed,
			parent.GasLimit,
			params.ApricotPhase1GasLimit,
			params.ApricotPhase1GasLimit,
		), nil
	}
}

// gasLimit was copied from core.CalcGasLimit to avoid a circular dependency.
func gasLimit(parentGasUsed, parentGasLimit, gasFloor, gasCeil uint64) uint64 {
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
	limit := max(parentGasLimit-decay+contrib, params.MinGasLimit)
	// If we're outside our allowed gas range, we try to hone towards them
	if limit < gasFloor {
		limit = min(parentGasLimit+decay, gasFloor)
	} else if limit > gasCeil {
		limit = max(parentGasLimit-decay, gasCeil)
	}
	return limit
}

func VerifyGasLimit(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	switch {
	case config.IsFUpgrade(header.Time):
		gasState, err := CalculateDynamicFeeAccumulator(config, parent, header.Time)
		if err != nil {
			return err
		}
		if header.GasLimit != uint64(gasState.Gas.Capacity) {
			return fmt.Errorf("invalid gas limit: have %d, want %d", header.GasLimit, gasState.Gas.Capacity)
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
		if diff >= limit || header.GasLimit < params.MinGasLimit || header.GasLimit > params.MaxGasLimit {
			return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
		}
	}
	return nil
}
