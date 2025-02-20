// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

var errInvalidExtraLength = errors.New("invalid header.Extra length")

// ExtraPrefix returns what the prefix of the header's Extra field should be
// based on the desired target excess.
//
// If the `desiredTargetExcess` is nil, the parent's target excess is used.
func ExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
	desiredTargetExcess *gas.Gas,
) ([]byte, error) {
	switch {
	case config.IsFUpgrade(header.Time):
		// Calculate the gas state for the start of the block
		gasState, err := feeStateBeforeBlock(config, parent, header.Time)
		if err != nil {
			return nil, err
		}
		if err := gasState.ConsumeGas(header.GasUsed, header.ExtDataGasUsed); err != nil {
			return nil, err
		}
		// If the desired target excess isn't specified, default to the parent
		// target excess.
		if desiredTargetExcess != nil {
			gasState.UpdateTargetExcess(*desiredTargetExcess)
		}
		return feeExcessBytes(gasState), nil
	case config.IsApricotPhase3(header.Time):
		window, err := feeWindow(config, parent, header.Time)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return feeWindowBytes(window), nil
	default:
		// Prior to AP3 there was no expected extra prefix.
		return nil, nil
	}
}

// VerifyExtraPrefix verifies that the header's Extra field is correctly
// formatted.
func VerifyExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	switch {
	case config.IsFUpgrade(header.Time):
		state, err := feeStateAfterBlock(
			header.GasLimit,
			header.GasUsed,
			header.ExtDataGasUsed,
			header.Extra,
		)
		if err != nil {
			return err
		}

		// Calculate the expected gas state for after the block
		expectedState, err := feeStateBeforeBlock(config, parent, header.Time)
		if err != nil {
			return err
		}
		if err := expectedState.ConsumeGas(header.GasUsed, header.ExtDataGasUsed); err != nil {
			return err
		}

		// By passing in the claimed target excess, we ensure that the expected
		// target excess is equal to the claimed target excess if it is possible
		// to have correctly set it to that value. Otherwise, the resulting
		// value will be as close to the claimed value as possible, but would
		// not be equal.
		expectedState.UpdateTargetExcess(state.TargetExcess)

		if state.Gas.Excess != expectedState.Gas.Excess {
			return fmt.Errorf("invalid gas state excess: have %d, want %d", state.Gas.Excess, expectedState.Gas.Excess)
		}
		if state.TargetExcess != expectedState.TargetExcess {
			return fmt.Errorf("invalid gas state target excess: have %d, want %d", state.TargetExcess, expectedState.TargetExcess)
		}
	case config.IsApricotPhase3(header.Time):
		feeWindow, err := feeWindow(config, parent, header.Time)
		if err != nil {
			return err
		}
		feeWindowBytes := feeWindowBytes(feeWindow)
		if !bytes.HasPrefix(header.Extra, feeWindowBytes) {
			return fmt.Errorf("expected header prefix: %x, found %x", feeWindowBytes, header.Extra)
		}
	}
	return nil
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// rules.
//
// TODO: Should this be merged with VerifyExtraPrefix?
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsFUpgrade:
		if extraLen < FeeExcessSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				FeeExcessSize,
				extraLen,
			)
		}
	case rules.IsDurango:
		if extraLen < FeeWindowSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				FeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase3:
		if extraLen != FeeWindowSize {
			return fmt.Errorf(
				"%w: expected %d but got %d",
				errInvalidExtraLength,
				FeeWindowSize,
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
