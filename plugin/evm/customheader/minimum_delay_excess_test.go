// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
)

func generateHeaderWithMinDelayExcess(timeSeconds uint64, minDelayExcess *uint64) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{
			Time: timeSeconds,
		},
		&customtypes.HeaderExtra{
			MinDelayExcess: minDelayExcess,
		},
	)
}

func TestMinDelayExcess(t *testing.T) {
	activatingGraniteConfig := *extras.TestGraniteChainConfig
	activatingGraniteTimestamp := uint64(1000)
	activatingGraniteConfig.NetworkUpgrades.GraniteTimestamp = utils.NewUint64(activatingGraniteTimestamp)

	tests := []struct {
		name                  string
		config                *extras.ChainConfig
		parent                *types.Header
		header                *types.Header
		desiredMinDelayExcess *uint64
		expectedDelayExcess   *uint64
		expectedErr           error
	}{
		// Pre-Granite tests
		{
			name:   "pre_granite_returns_nil",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   nil,
			expectedErr:           nil,
		},
		{
			name:   "pre_granite_with_desired_value_returns_nil",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: utils.NewUint64(1000),
			expectedDelayExcess:   nil,
			expectedErr:           nil,
		},
		{
			name:   "granite_first_block_initial_delay_excess",
			config: &activatingGraniteConfig,
			parent: &types.Header{
				Time: activatingGraniteTimestamp - 1,
			},
			header: &types.Header{
				Time: activatingGraniteTimestamp + 1,
			},
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   utils.NewUint64(acp226.InitialDelayExcess),
			expectedErr:           nil,
		},
		{
			name:   "granite_no_parent_min_delay_error",
			config: extras.TestGraniteChainConfig,
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   nil,
			expectedErr:           errParentMinDelayExcessNil,
		},
		{
			name:   "granite_with_parent_min_delay",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   utils.NewUint64(500),
			expectedErr:           nil,
		},
		{
			name:   "granite_with_desired_min_delay_excess",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: utils.NewUint64(1000),
			expectedDelayExcess:   utils.NewUint64(500 + acp226.MaxDelayExcessDiff),
			expectedErr:           nil,
		},
		{
			name:   "granite_with_zero_desired_value",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: utils.NewUint64(0),
			expectedDelayExcess:   utils.NewUint64(500 - acp226.MaxDelayExcessDiff),
			expectedErr:           nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := MinDelayExcess(test.config, test.parent, test.header, test.desiredMinDelayExcess)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expectedDelayExcess, result)
		})
	}
}

func TestVerifyMinDelayExcess(t *testing.T) {
	tests := []struct {
		name        string
		config      *extras.ChainConfig
		parent      *types.Header
		header      *types.Header
		expectedErr error
	}{
		{
			name:   "pre_granite_no_verification",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			expectedErr: nil,
		},
		{
			name:   "granite_nil_min_delay_excess_error",
			config: extras.TestGraniteChainConfig,
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			expectedErr: errRemoteMinDelayExcessNil,
		},
		{
			name:        "granite_incorrect_min_delay_excess",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header:      generateHeaderWithMinDelayExcess(1001, utils.NewUint64(1000)),
			expectedErr: errIncorrectMinDelayExcess,
		},
		{
			name:        "granite_incorrect_min_delay_excess_with_zero_desired",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header:      generateHeaderWithMinDelayExcess(1001, utils.NewUint64(0)),
			expectedErr: errIncorrectMinDelayExcess,
		},
		{
			name:        "granite_correct_min_delay_excess",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header:      generateHeaderWithMinDelayExcess(1001, utils.NewUint64(500)),
			expectedErr: nil,
		},
		{
			name:        "granite_with_increased_desired_min_delay_excess_correct",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header:      generateHeaderWithMinDelayExcess(1001, utils.NewUint64(700)),
			expectedErr: nil,
		},
		{
			name:        "granite_with_decreased_desired_min_delay_excess_correct",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, utils.NewUint64(500)),
			header:      generateHeaderWithMinDelayExcess(1001, utils.NewUint64(300)),
			expectedErr: nil,
		},

		// Different chain configs
		{
			name:        "fortuna_config_no_verification",
			config:      extras.TestFortunaChainConfig,
			parent:      &types.Header{Time: 1000},
			header:      &types.Header{Time: 1001},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.expectedErr != nil && test.expectedErr.Error() == "panic expected" {
				// Test panic cases
				require.Panics(t, func() {
					VerifyMinDelayExcess(test.config, test.parent, test.header)
				})
				return
			}

			err := VerifyMinDelayExcess(test.config, test.parent, test.header)

			if test.expectedErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
