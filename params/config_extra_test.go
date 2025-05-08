// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

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
