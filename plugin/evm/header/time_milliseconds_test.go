// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/params/extras/extrastest"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
)

func TestTimeMillisecondsPart(t *testing.T) {
	tests := map[string]struct {
		fork     upgradetest.Fork
		tstart   time.Time
		expected *uint64
	}{
		"pre_granite_time_milliseconds_should_be_nil": {
			fork:     upgradetest.Fortuna,
			tstart:   time.Unix(1714339200, 0),
			expected: nil,
		},
		"granite_time_milliseconds_should_be_non_nil": {
			fork:     upgradetest.Granite,
			tstart:   time.Unix(1714339200, 123_456_789),
			expected: utils.NewUint64(123),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rules := extrastest.GetAvalancheRulesFromFork(test.fork)
			timeMillisecondsPart := TimeMillisecondsPart(rules, test.tstart)
			require.Equal(t, test.expected, timeMillisecondsPart)
		})
	}
}

func TestVerifyTimeMilliseconds(t *testing.T) {
	tests := map[string]struct {
		timeMillisecondsPart *uint64
		rules                extras.AvalancheRules
		expectedErr          error
	}{
		"pre_granite_time_milliseconds_should_fail": {
			timeMillisecondsPart: utils.NewUint64(0),
			rules:                extrastest.GetAvalancheRulesFromFork(upgradetest.Fortuna),
			expectedErr:          ErrTimeMillisecondsBeforeGranite,
		},
		"pre_granite_time_nil_milliseconds_should_work": {
			timeMillisecondsPart: nil,
			rules:                extrastest.GetAvalancheRulesFromFork(upgradetest.Fortuna),
			expectedErr:          nil,
		},
		"granite_time_milliseconds_should_be_non_nil_and_fail": {
			timeMillisecondsPart: nil,
			rules:                extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr:          ErrTimeMillisecondsRequired,
		},
		"granite_time_milliseconds_should_not_exceed_999_and_fail": {
			timeMillisecondsPart: utils.NewUint64(1000),
			rules:                extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr:          ErrTimeMillisecondsInvalid,
		},
		"granite_time_milliseconds_should_be_between_0_999_and_work": {
			timeMillisecondsPart: utils.NewUint64(100),
			rules:                extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr:          nil,
		},
		"granite_time_milliseconds_0_should_work": {
			timeMillisecondsPart: utils.NewUint64(0),
			rules:                extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr:          nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			header := customtypes.WithHeaderExtra(
				&types.Header{
					Time: 0,
				},
				&customtypes.HeaderExtra{
					TimeMillisecondsPart: test.timeMillisecondsPart,
				},
			)
			err := VerifyTimeMilliseconds(test.rules, header)
			if test.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.expectedErr)
			}
		})
	}
}
