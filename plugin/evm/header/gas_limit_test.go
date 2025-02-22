// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/cortina"
	"github.com/stretchr/testify/require"
)

func TestGasLimit(t *testing.T) {
	tests := []struct {
		name      string
		upgrades  params.NetworkUpgrades
		parent    *types.Header
		timestamp uint64
		want      uint64
		wantErr   error
	}{
		{
			name:     "f_invalid_parent_header",
			upgrades: params.TestFUpgradeChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: errFeeStateInsufficientLength,
		},
		{
			name:     "f_initial_max_capacity",
			upgrades: params.TestFUpgradeChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: acp176.MinTargetPerSecond * acp176.TargetToMax * acp176.TimeToFillCapacity,
		},
		{
			name:     "cortina",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			want:     cortina.GasLimit,
		},
		{
			name:     "ap1",
			upgrades: params.TestApricotPhase1Config.NetworkUpgrades,
			want:     ap1.GasLimit,
		},
		{
			name:     "launch",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: 1,
			},
			want: 1, // Same as parent
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := GasLimit(config, test.parent, test.timestamp)
			require.Equal(test.want, got)
			require.ErrorIs(err, test.wantErr)
		})
	}
}

func TestVerifyGasLimit(t *testing.T) {
	tests := []struct {
		name     string
		upgrades params.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     error
	}{
		{
			name:     "cortina_valid",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasLimit: cortina.GasLimit,
			},
		},
		{
			name:     "cortina_invalid",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasLimit: cortina.GasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "ap1_valid",
			upgrades: params.TestApricotPhase1Config.NetworkUpgrades,
			header: &types.Header{
				GasLimit: ap1.GasLimit,
			},
		},
		{
			name:     "ap1_invalid",
			upgrades: params.TestApricotPhase1Config.NetworkUpgrades,
			header: &types.Header{
				GasLimit: ap1.GasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "launch_valid",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: 50_000,
			},
			header: &types.Header{
				GasLimit: 50_001, // Gas limit is allowed to change by 1/1024
			},
		},
		{
			name:     "launch_too_low",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: params.MinGasLimit,
			},
			header: &types.Header{
				GasLimit: params.MinGasLimit - 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "launch_too_high",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: params.MaxGasLimit,
			},
			header: &types.Header{
				GasLimit: params.MaxGasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "change_too_large",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: params.MinGasLimit,
			},
			header: &types.Header{
				GasLimit: params.MaxGasLimit,
			},
			want: errInvalidGasLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			err := VerifyGasLimit(config, test.parent, test.header)
			require.ErrorIs(t, err, test.want)
		})
	}
}
