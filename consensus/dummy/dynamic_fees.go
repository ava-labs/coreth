// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/utils"

	customheader "github.com/ava-labs/coreth/plugin/evm/header"
)

var (
	errBlockGasCostNil        = errors.New("block gas cost is nil")
	errBaseFeeNil             = errors.New("base fee is nil")
	errExtDataGasUsedNil      = errors.New("extDataGasUsed is nil")
	errExtDataGasUsedTooLarge = errors.New("extDataGasUsed is not uint64")

	errEstimateBaseFeeWithoutActivation = errors.New("cannot estimate base fee for chain without apricot phase 3 scheduled")
)

func VerifyHeaderGasFields(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	if err := customheader.VerifyGasLimit(config, parent, header); err != nil {
		return err
	}

	switch {
	case config.IsFUpgrade(header.Time):
		gasState, err := customheader.ParseDynamicFeeAccumulator(
			header.GasLimit,
			header.GasUsed,
			header.ExtDataGasUsed,
			header.Extra,
		)
		if err != nil {
			return err
		}

		// Calculate the gas state for the start of the block
		expectedGasState, err := customheader.CalculateDynamicFeeAccumulator(config, parent, header.Time)
		if err != nil {
			return err
		}
		if err := expectedGasState.ConsumeGas(header.GasUsed, header.ExtDataGasUsed); err != nil {
			return err
		}
		expectedGasState.UpdateTargetExcess(gasState.TargetExcess)

		if gasState.Gas.Excess != expectedGasState.Gas.Excess {
			return fmt.Errorf("invalid gas state excess: have %v, want %v", gasState.Gas.Excess, expectedGasState.Gas.Excess)
		}
		if gasState.TargetExcess != expectedGasState.TargetExcess {
			return fmt.Errorf("invalid gas state target excess: have %v, want %v", gasState.TargetExcess, expectedGasState.TargetExcess)
		}
	case config.IsApricotPhase3(header.Time):
		feeWindow, err := customheader.CalculateDynamicFeeWindow(config, parent, header.Time)
		if err != nil {
			return err
		}
		feeWindowBytes := customheader.DynamicFeeWindowBytes(feeWindow)
		if !bytes.HasPrefix(header.Extra, feeWindowBytes) {
			return fmt.Errorf("expected header prefix: %x, found %x", feeWindowBytes, header.Extra)
		}
	}

	expectedBaseFee, err := customheader.BaseFee(config, parent, header.Time)
	if err != nil {
		return fmt.Errorf("failed to calculate base fee: %w", err)
	}
	if !utils.BigEqual(header.BaseFee, expectedBaseFee) {
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
	expectedBlockGasCost := customheader.BlockGasCost(config, parent, header.Time)
	if !utils.BigEqualUint64(header.BlockGasCost, expectedBlockGasCost) {
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
