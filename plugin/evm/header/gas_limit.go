// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/cortina"
)

var errInvalidGasLimit = errors.New("invalid gas limit")

// GasLimit takes the previous header and the timestamp of its child block and
// calculates the gas limit for the child block.
func GasLimit(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (uint64, error) {
	switch {
	case config.IsFUpgrade(timestamp):
		gasState, err := feeStateBeforeBlock(config, parent, timestamp)
		if err != nil {
			return 0, err
		}
		return uint64(gasState.Gas.Capacity), nil
	case config.IsCortina(timestamp):
		return cortina.GasLimit, nil
	case config.IsApricotPhase1(timestamp):
		return ap1.GasLimit, nil
	default:
		// The gas limit prior Apricot Phase 1 started at the genesis value and
		// migrated towards the [ap1.GasLimit] following the `core.CalcGasLimit`
		// updates. However, since all chains have activated Apricot Phase 1,
		// this code is not used in production. To avoid a dependency on the
		// `core` package, this code is modified to just return the parent gas
		// limit; which was valid to do prior to Apricot Phase 1.
		return parent.GasLimit, nil
	}
}

// VerifyGasUsed verifies that the gas used is less than or equal to the gas
// limit.
func VerifyGasUsed(
	config *params.ChainConfig,
	header *types.Header,
) error {
	gasUsed := header.GasUsed
	if config.IsFUpgrade(header.Time) && header.ExtDataGasUsed != nil {
		if !header.ExtDataGasUsed.IsUint64() {
			return fmt.Errorf("invalid gas limit: invalid extra data gas used %d", header.ExtDataGasUsed)
		}
		var err error
		gasUsed, err = math.Add(gasUsed, header.ExtDataGasUsed.Uint64())
		if err != nil {
			return err
		}
	}
	if gasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", gasUsed, header.GasLimit)
	}
	return nil
}

// VerifyGasLimit verifies that the gas limit for the header is valid.
func VerifyGasLimit(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	switch {
	case config.IsFUpgrade(header.Time):
		gasState, err := feeStateBeforeBlock(config, parent, header.Time)
		if err != nil {
			return err
		}
		if header.GasLimit != uint64(gasState.Gas.Capacity) {
			return fmt.Errorf("%w: have %d, want %d",
				errInvalidGasLimit,
				header.GasLimit,
				gasState.Gas.Capacity,
			)
		}
	case config.IsCortina(header.Time):
		if header.GasLimit != cortina.GasLimit {
			return fmt.Errorf("%w: expected to be %d in Cortina, but found %d",
				errInvalidGasLimit,
				cortina.GasLimit,
				header.GasLimit,
			)
		}
	case config.IsApricotPhase1(header.Time):
		if header.GasLimit != ap1.GasLimit {
			return fmt.Errorf("%w: expected to be %d in ApricotPhase1, but found %d",
				errInvalidGasLimit,
				ap1.GasLimit,
				header.GasLimit,
			)
		}
	default:
		if header.GasLimit < params.MinGasLimit || header.GasLimit > params.MaxGasLimit {
			return fmt.Errorf("%w: %d not in range [%d, %d]",
				errInvalidGasLimit,
				header.GasLimit,
				params.MinGasLimit,
				params.MaxGasLimit,
			)
		}

		// Verify that the gas limit remains within allowed bounds
		diff := math.AbsDiff(parent.GasLimit, header.GasLimit)
		limit := parent.GasLimit / params.GasLimitBoundDivisor
		if diff >= limit {
			return fmt.Errorf("%w: have %d, want %d += %d",
				errInvalidGasLimit,
				header.GasLimit,
				parent.GasLimit,
				limit,
			)
		}
	}
	return nil
}
