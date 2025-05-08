// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/utils"
	"github.com/stretchr/testify/require"
)

func TestSetEthUpgrades(t *testing.T) {
	genesisBlock := big.NewInt(0)
	genesisTimestamp := utils.NewUint64(initiallyActive)
	tests := []struct {
		fork     upgradetest.Fork
		expected *ChainConfig
	}{
		{
			fork: upgradetest.NoUpgrades,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
			},
		},
		{
			fork: upgradetest.ApricotPhase1,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
			},
		},
		{
			fork: upgradetest.ApricotPhase2,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
			},
		},
		{
			fork: upgradetest.ApricotPhase3,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         genesisBlock,
			},
		},
		{
			fork: upgradetest.Durango,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         genesisBlock,
				ShanghaiTime:        genesisTimestamp,
			},
		},
		{
			fork: upgradetest.Etna,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         genesisBlock,
				ShanghaiTime:        genesisTimestamp,
				CancunTime:          genesisTimestamp,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.fork.String(), func(t *testing.T) {
			require := require.New(t)

			extraConfig := &extras.ChainConfig{
				NetworkUpgrades: extras.GetNetworkUpgrades(upgradetest.GetConfig(test.fork)),
			}
			initial := WithExtra(
				&ChainConfig{},
				extraConfig,
			)
			require.NoError(SetEthUpgrades(initial))

			expected := WithExtra(
				test.expected,
				extraConfig,
			)
			require.Equal(expected, initial)
		})
	}
}
