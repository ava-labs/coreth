// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
)

const nAVAX = 1_000_000_000

var (
	minBaseFee       = big.NewInt(params.EtnaMinBaseFee) // 1 nAVAX
	maxNormalBaseFee = big.NewInt(100 * nAVAX)

	minGasTip       = common.Big1
	maxNormalGasTip = big.NewInt(20 * nAVAX)

	slowFeeNum = big.NewInt(19) // 19/20 = 0.95
	fastFeeNum = big.NewInt(21) // 21/20 = 1.05
	feeDen     = big.NewInt(20)
)

type Price struct {
	GasTip *hexutil.Big `json:"maxPriorityFeePerGas"`
	GasFee *hexutil.Big `json:"maxFeePerGas"`
}

type PriceOptions struct {
	Slow   *Price `json:"slow"`
	Normal *Price `json:"normal"`
	Fast   *Price `json:"fast"`
}

// SuggestPriceOptions returns suggestions for what to display to a user for
// current transaction fees.
func (s *EthereumAPI) SuggestPriceOptions(ctx context.Context) (*PriceOptions, error) {
	baseFee, err := s.b.EstimateBaseFee(ctx)
	if err != nil || baseFee == nil {
		return nil, err
	}
	gasTip, err := s.b.SuggestGasTipCap(ctx)
	if err != nil || gasTip == nil {
		return nil, err
	}

	slowBaseFee, normalBaseFee, fastBaseFee := priceOptions(
		minBaseFee,
		baseFee,
		maxNormalBaseFee,
	)
	slowGasTip, normalGasTip, fastGasTip := priceOptions(
		minGasTip,
		gasTip,
		maxNormalGasTip,
	)
	slowGasFee := new(big.Int).Add(slowBaseFee, slowGasTip)
	normalGasFee := new(big.Int).Add(normalBaseFee, normalGasTip)
	fastGasFee := new(big.Int).Add(fastBaseFee, fastGasTip)
	return &PriceOptions{
		Slow: &Price{
			GasTip: (*hexutil.Big)(slowGasTip),
			GasFee: (*hexutil.Big)(slowGasFee),
		},
		Normal: &Price{
			GasTip: (*hexutil.Big)(normalGasTip),
			GasFee: (*hexutil.Big)(normalGasFee),
		},
		Fast: &Price{
			GasTip: (*hexutil.Big)(fastGasTip),
			GasFee: (*hexutil.Big)(fastGasFee),
		},
	}, nil
}

func priceOptions(
	minFee *big.Int,
	estimate *big.Int,
	maxFee *big.Int,
) (*big.Int, *big.Int, *big.Int) {
	// Cap the fee to keep slow and normal options reasonable during fee spikes.
	cappedFee := math.BigMin(estimate, maxFee)

	slowFee := new(big.Int).Set(cappedFee)
	slowFee.Mul(slowFee, slowFeeNum)
	slowFee.Div(slowFee, feeDen)
	slowFee = math.BigMax(slowFee, minFee)

	normalFee := cappedFee

	fastFee := new(big.Int).Set(estimate)
	fastFee.Mul(fastFee, fastFeeNum)
	fastFee.Div(fastFee, feeDen)
	return slowFee, normalFee, fastFee
}
