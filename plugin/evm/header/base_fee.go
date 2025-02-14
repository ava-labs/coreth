// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

// BaseFee takes the previous header and the timestamp of its child block and
// calculates the expected base fee for the child block.
func BaseFee(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (*big.Int, error) {
	switch {
	case config.IsFUpgrade(timestamp):
		gasState, err := CalculateDynamicFeeAccumulator(config, parent, timestamp)
		if err != nil {
			return nil, err
		}
		return new(big.Int).SetUint64(uint64(gasState.GasPrice())), nil
	case config.IsApricotPhase3(timestamp):
		return calcBaseFeeWithWindow(config, parent, timestamp)
	default:
		return nil, nil
	}
}

// EstimateNextBaseFee attempts to estimate the base fee of a block built at
// `timestamp` on top of `parent`.
//
// If timestamp is before parent.Time or the AP3 activation time, then timestamp
// is set to the maximum of parent.Time and the AP3 activation time.
//
// Warning: This function should only be used in estimation and should not be
// used when calculating the canonical base fee for a block.
func EstimateNextBaseFee(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (*big.Int, error) {
	if config.ApricotPhase3BlockTimestamp == nil {
		return nil, errEstimateBaseFeeWithoutActivation
	}

	timestamp = max(timestamp, parent.Time, *config.ApricotPhase3BlockTimestamp)
	return BaseFee(config, parent, timestamp)
}
