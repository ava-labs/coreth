// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/precompile/contracts/warp"
	"github.com/ava-labs/coreth/utils"
)

func TestIsTimestampForked(t *testing.T) {
	type test struct {
		fork     *uint64
		block    uint64
		isForked bool
	}

	for name, test := range map[string]test{
		"nil fork at 0": {
			fork:     nil,
			block:    0,
			isForked: false,
		},
		"nil fork at non-zero": {
			fork:     nil,
			block:    100,
			isForked: false,
		},
		"zero fork at genesis": {
			fork:     utils.NewUint64(0),
			block:    0,
			isForked: true,
		},
		"pre fork timestamp": {
			fork:     utils.NewUint64(100),
			block:    50,
			isForked: false,
		},
		"at fork timestamp": {
			fork:     utils.NewUint64(100),
			block:    100,
			isForked: true,
		},
		"post fork timestamp": {
			fork:     utils.NewUint64(100),
			block:    150,
			isForked: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := isTimestampForked(test.fork, test.block)
			assert.Equal(t, test.isForked, res)
		})
	}
}

func TestIsForkTransition(t *testing.T) {
	type test struct {
		fork, parent *uint64
		current      uint64
		transitioned bool
	}

	for name, test := range map[string]test{
		"not active at genesis": {
			fork:         nil,
			parent:       nil,
			current:      0,
			transitioned: false,
		},
		"activate at genesis": {
			fork:         utils.NewUint64(0),
			parent:       nil,
			current:      0,
			transitioned: true,
		},
		"nil fork arbitrary transition": {
			fork:         nil,
			parent:       utils.NewUint64(100),
			current:      101,
			transitioned: false,
		},
		"nil fork transition same timestamp": {
			fork:         nil,
			parent:       utils.NewUint64(100),
			current:      100,
			transitioned: false,
		},
		"exact match on current timestamp": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(99),
			current:      100,
			transitioned: true,
		},
		"current same as parent does not transition twice": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(101),
			current:      101,
			transitioned: false,
		},
		"current, parent, and fork same should not transition twice": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(100),
			current:      100,
			transitioned: false,
		},
		"current transitions after fork": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(99),
			current:      101,
			transitioned: true,
		},
		"current and parent come after fork": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(101),
			current:      102,
			transitioned: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := IsForkTransition(test.fork, test.parent, test.current)
			assert.Equal(t, test.transitioned, res)
		})
	}
}

func TestCheckPrecompilesCompatible(t *testing.T) {
	type test struct {
		name                string
		storedUpgrades      []PrecompileUpgrade
		newUpgrades         []PrecompileUpgrade
		headTimestamp       uint64
		wantErr             bool
		expectedErrContains string
	}

	tests := []test{
		{
			name:           "no upgrades to no upgrades - should succeed",
			storedUpgrades: []PrecompileUpgrade{},
			newUpgrades:    []PrecompileUpgrade{},
			headTimestamp:  100,
			wantErr:        false,
		},
		{
			name:           "adding upgrades to empty config - should succeed (new logic)",
			storedUpgrades: []PrecompileUpgrade{},
			newUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
			},
			headTimestamp: 100, // Past the activation time, but should still succeed
			wantErr:       false,
		},
		{
			name:           "adding multiple upgrades to empty config - should succeed",
			storedUpgrades: []PrecompileUpgrade{},
			newUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
				{Config: warp.NewConfig(utils.NewUint64(80), 0, true)}, // disable at 80
			},
			headTimestamp: 100,
			wantErr:       false,
		},
		{
			name: "adding upgrades to existing config - should fail if retroactive",
			storedUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(30), 0, false)},
			},
			newUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(30), 0, false)}, // same as stored
				{Config: warp.NewConfig(utils.NewUint64(50), 0, true)},  // new retroactive upgrade
			},
			headTimestamp:       100,
			wantErr:             true,
			expectedErrContains: "cannot retroactively enable PrecompileUpgrade[1]",
		},
		{
			name: "modifying existing activated upgrade - should fail",
			storedUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
			},
			newUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 1, false)}, // different admin address
			},
			headTimestamp:       100,
			wantErr:             true,
			expectedErrContains: "PrecompileUpgrade[0]",
		},
		{
			name: "removing existing activated upgrade - should fail",
			storedUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
			},
			newUpgrades:         []PrecompileUpgrade{}, // removed the upgrade
			headTimestamp:       100,
			wantErr:             true,
			expectedErrContains: "missing PrecompileUpgrade[0]",
		},
		{
			name: "adding non-retroactive upgrades to existing config - should succeed",
			storedUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
			},
			newUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)}, // same as stored
				{Config: warp.NewConfig(utils.NewUint64(150), 0, true)}, // future upgrade
			},
			headTimestamp: 100,
			wantErr:       false,
		},
		{
			name: "identical configs - should succeed",
			storedUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
				{Config: warp.NewConfig(utils.NewUint64(80), 0, true)},
			},
			newUpgrades: []PrecompileUpgrade{
				{Config: warp.NewConfig(utils.NewUint64(50), 0, false)},
				{Config: warp.NewConfig(utils.NewUint64(80), 0, true)},
			},
			headTimestamp: 100,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create stored config with the stored upgrades
			storedConfig := &ChainConfig{
				UpgradeConfig: UpgradeConfig{
					PrecompileUpgrades: tt.storedUpgrades,
				},
			}

			// Test the compatibility check
			err := storedConfig.checkPrecompilesCompatible(tt.newUpgrades, tt.headTimestamp)

			if tt.wantErr {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.expectedErrContains != "" {
					assert.Contains(t, err.Error(), tt.expectedErrContains, "Error message should contain expected text")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for test case %s, but got: %v", tt.name, err)
				}
			}
		})
	}
}
