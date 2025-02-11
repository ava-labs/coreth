// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

type testSuggestPriceOptionsBackend struct {
	Backend // embeds the interface to avoid implementing unused methods

	estimateBaseFee  *big.Int
	suggestGasTipCap *big.Int
}

func (b *testSuggestPriceOptionsBackend) EstimateBaseFee(context.Context) (*big.Int, error) {
	return b.estimateBaseFee, nil
}

func (b *testSuggestPriceOptionsBackend) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return b.suggestGasTipCap, nil
}

func TestSuggestPriceOptions(t *testing.T) {
	tests := []struct {
		name             string
		estimateBaseFee  *big.Int
		suggestGasTipCap *big.Int
		expected         *PriceOptions
	}{
		{
			name:             "nil_base_fee",
			estimateBaseFee:  nil,
			suggestGasTipCap: common.Big1,
			expected:         nil,
		},
		{
			name:             "nil_tip_cap",
			estimateBaseFee:  common.Big1,
			suggestGasTipCap: nil,
			expected:         nil,
		},
		{
			name:             "minimum_values",
			estimateBaseFee:  bigMinBaseFee,
			suggestGasTipCap: bigMinGasTip,
			expected: &PriceOptions{
				Slow: &Price{
					GasTip: (*hexutil.Big)(
						bigMinGasTip,
					),
					GasFee: (*hexutil.Big)(big.NewInt(
						minBaseFee + minGasTip,
					)),
				},
				Normal: &Price{
					GasTip: (*hexutil.Big)(
						bigMinGasTip,
					),
					GasFee: (*hexutil.Big)(big.NewInt(
						minBaseFee + minGasTip,
					)),
				},
				Fast: &Price{
					GasTip: (*hexutil.Big)(
						bigMinGasTip,
					),
					GasFee: (*hexutil.Big)(big.NewInt(
						(fastFeeNum*minBaseFee)/feeDen + (fastFeeNum*minGasTip)/feeDen,
					)),
				},
			},
		},
		{
			name:             "maximum_values",
			estimateBaseFee:  bigMaxNormalBaseFee,
			suggestGasTipCap: bigMaxNormalGasTip,
			expected: &PriceOptions{
				Slow: &Price{
					GasTip: (*hexutil.Big)(big.NewInt(
						(slowFeeNum * maxNormalGasTip) / feeDen,
					)),
					GasFee: (*hexutil.Big)(big.NewInt(
						(slowFeeNum*maxNormalBaseFee)/feeDen + (slowFeeNum*maxNormalGasTip)/feeDen,
					)),
				},
				Normal: &Price{
					GasTip: (*hexutil.Big)(
						bigMaxNormalGasTip,
					),
					GasFee: (*hexutil.Big)(big.NewInt(
						maxNormalBaseFee + maxNormalGasTip,
					)),
				},
				Fast: &Price{
					GasTip: (*hexutil.Big)(big.NewInt(
						(fastFeeNum * maxNormalGasTip) / feeDen,
					)),
					GasFee: (*hexutil.Big)(big.NewInt(
						(fastFeeNum*maxNormalBaseFee)/feeDen + (fastFeeNum*maxNormalGasTip)/feeDen,
					)),
				},
			},
		},
		{
			name:             "double_maximum_values",
			estimateBaseFee:  big.NewInt(2 * maxNormalBaseFee),
			suggestGasTipCap: big.NewInt(2 * maxNormalGasTip),
			expected: &PriceOptions{
				Slow: &Price{
					GasTip: (*hexutil.Big)(big.NewInt(
						(slowFeeNum * maxNormalGasTip) / feeDen,
					)),
					GasFee: (*hexutil.Big)(big.NewInt(
						(slowFeeNum*maxNormalBaseFee)/feeDen + (slowFeeNum*maxNormalGasTip)/feeDen,
					)),
				},
				Normal: &Price{
					GasTip: (*hexutil.Big)(
						bigMaxNormalGasTip,
					),
					GasFee: (*hexutil.Big)(big.NewInt(
						maxNormalBaseFee + maxNormalGasTip,
					)),
				},
				Fast: &Price{
					GasTip: (*hexutil.Big)(big.NewInt(
						(fastFeeNum * 2 * maxNormalGasTip) / feeDen,
					)),
					GasFee: (*hexutil.Big)(big.NewInt(
						(fastFeeNum*2*maxNormalBaseFee)/feeDen + (fastFeeNum*2*maxNormalGasTip)/feeDen,
					)),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			backend := &testSuggestPriceOptionsBackend{
				estimateBaseFee:  test.estimateBaseFee,
				suggestGasTipCap: test.suggestGasTipCap,
			}
			api := NewEthereumAPI(backend)

			options, err := api.SuggestPriceOptions(context.Background())
			require.NoError(err)
			require.Equal(test.expected, options)
		})
	}
}
