// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
)

const ApricotPhase3BlockGasFee = 1_000_000

var (
	MaxUint256Plus1 = new(big.Int).Lsh(common.Big1, 256)
	MaxUint256      = new(big.Int).Sub(MaxUint256Plus1, common.Big1)

	ApricotPhase3MinBaseFee     = big.NewInt(params.ApricotPhase3MinBaseFee)
	ApricotPhase3MaxBaseFee     = big.NewInt(params.ApricotPhase3MaxBaseFee)
	ApricotPhase4MinBaseFee     = big.NewInt(params.ApricotPhase4MinBaseFee)
	ApricotPhase4MaxBaseFee     = big.NewInt(params.ApricotPhase4MaxBaseFee)
	ApricotPhase3InitialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
	EtnaMinBaseFee              = big.NewInt(params.EtnaMinBaseFee)

	ApricotPhase4BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase4BaseFeeChangeDenominator)
	ApricotPhase5BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase5BaseFeeChangeDenominator)

	errEstimateBaseFeeWithoutActivation = errors.New("cannot estimate base fee for chain without apricot phase 3 scheduled")
)

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee for the child block.
//
// Prior to AP3, the returned base fee will be nil.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	switch {
	case config.IsApricotPhase3(timestamp):
		return calcBaseFeeWithWindow(config, parent, timestamp)
	default:
		// Prior to AP3 the expected base fee is nil.
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
func EstimateNextBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	if config.ApricotPhase3BlockTimestamp == nil {
		return nil, errEstimateBaseFeeWithoutActivation
	}

	timestamp = max(timestamp, parent.Time, *config.ApricotPhase3BlockTimestamp)
	return CalcBaseFee(config, parent, timestamp)
}
