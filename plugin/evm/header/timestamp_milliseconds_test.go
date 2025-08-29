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

func TestTimestampMilliseconds(t *testing.T) {
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
			expected: utils.NewUint64(1714339200123),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rules := extrastest.GetAvalancheRulesFromFork(test.fork)
			timestampMilliseconds := TimestampMilliseconds(rules, test.tstart)
			require.Equal(t, test.expected, timestampMilliseconds)
		})
	}
}

func TestVerifyTimestampMilliseconds(t *testing.T) {
	tests := map[string]struct {
		header      *types.Header
		rules       extras.AvalancheRules
		expectedErr error
	}{
		"pre_granite_time_milliseconds_should_fail": {
			header: customtypes.WithHeaderExtra(
				&types.Header{
					Time: 0,
				},
				&customtypes.HeaderExtra{
					TimestampMilliseconds: utils.NewUint64(0),
				}),
			rules:       extrastest.GetAvalancheRulesFromFork(upgradetest.Fortuna),
			expectedErr: ErrTimestampMillisecondsBeforeGranite,
		},
		"pre_granite_time_nil_milliseconds_should_work": {
			header:      &types.Header{},
			rules:       extrastest.GetAvalancheRulesFromFork(upgradetest.Fortuna),
			expectedErr: nil,
		},
		"granite_time_milliseconds_should_be_non_nil": {
			header: customtypes.WithHeaderExtra(
				&types.Header{
					Time: 0,
				},
				&customtypes.HeaderExtra{
					TimestampMilliseconds: nil,
				},
			),
			rules:       extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr: ErrTimestampMillisecondsRequired,
		},
		"granite_time_milliseconds_0_should_work": {
			header: customtypes.WithHeaderExtra(
				&types.Header{
					Time: 0,
				},
				&customtypes.HeaderExtra{
					TimestampMilliseconds: utils.NewUint64(0),
				},
			),
			rules:       extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr: nil,
		},
		"granite_time_milliseconds_mismatch_with_header_time_should_fail": {
			header: customtypes.WithHeaderExtra(
				&types.Header{
					Time: 0,
				},
				&customtypes.HeaderExtra{
					TimestampMilliseconds: utils.NewUint64(1714339200123),
				},
			),
			rules:       extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
			expectedErr: ErrTimestampMillisecondsMismatch,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := VerifyTimestampMilliseconds(test.rules, test.header)
			if test.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.expectedErr)
			}
		})
	}
}
