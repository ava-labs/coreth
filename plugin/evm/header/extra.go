// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

var errInvalidExtraLength = errors.New("invalid header.Extra length")

func ExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
	desiredTargetExcess *gas.Gas,
) ([]byte, error) {
	switch {
	case config.IsFUpgrade(header.Time):
		// Calculate the gas state for the start of the block
		gasState, err := CalculateDynamicFeeAccumulator(config, parent, header.Time)
		if err != nil {
			return nil, err
		}
		if err := gasState.ConsumeGas(header.GasUsed, header.ExtDataGasUsed); err != nil {
			return nil, err
		}
		// If the desired target excess isn't specified, default to the current
		// target excess.
		if desiredTargetExcess != nil {
			gasState.UpdateTargetExcess(*desiredTargetExcess)
		}

		return DynamicFeeAccumulatorBytes(gasState), nil
	case config.IsApricotPhase3(header.Time):
		feeWindow, err := CalculateDynamicFeeWindow(config, parent, header.Time)
		if err != nil {
			return nil, err
		}

		return DynamicFeeWindowBytes(feeWindow), nil
	default:
		return nil, nil
	}
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// [rules].
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsFUpgrade:
		if extraLen < DynamicFeeAccumulatorSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				DynamicFeeAccumulatorSize,
				extraLen,
			)
		}
	case rules.IsDurango:
		if extraLen < DynamicFeeWindowSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				DynamicFeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase3:
		if extraLen != DynamicFeeWindowSize {
			return fmt.Errorf(
				"%w: expected %d but got %d",
				errInvalidExtraLength,
				DynamicFeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase1:
		if extraLen != 0 {
			return fmt.Errorf(
				"%w: expected 0 but got %d",
				errInvalidExtraLength,
				extraLen,
			)
		}
	default:
		if uint64(extraLen) > params.MaximumExtraDataSize {
			return fmt.Errorf(
				"%w: expected <= %d but got %d",
				errInvalidExtraLength,
				params.MaximumExtraDataSize,
				extraLen,
			)
		}
	}
	return nil
}
