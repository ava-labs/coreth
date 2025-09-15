// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

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

func TestTimeMilliseconds(t *testing.T) {
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
			timeMilliseconds := TimeMilliseconds(rules, test.tstart)
			require.Equal(t, test.expected, timeMilliseconds)
		})
	}
}

func TestVerifyTime(t *testing.T) {
	var (
		time        = time.Unix(1714339200, 123_456_789)
		timeSeconds = uint64(time.Unix())
		timeMillis  = uint64(time.UnixMilli())
	)
	tests := []struct {
		name             string
		timeSeconds      uint64
		timeMilliseconds *uint64
		extraConfig      *extras.ChainConfig
		expectedErr      error
	}{
		{
			name:             "pre_granite_time_milliseconds_should_fail",
			timeSeconds:      timeSeconds,
			timeMilliseconds: utils.NewUint64(timeMillis),
			extraConfig:      extras.TestFortunaChainConfig,
			expectedErr:      ErrTimeMillisecondsBeforeGranite,
		},
		{
			name:             "pre_granite_time_nil_milliseconds_should_work",
			timeSeconds:      timeSeconds,
			timeMilliseconds: nil,
			extraConfig:      extras.TestFortunaChainConfig,
			expectedErr:      nil,
		},
		{
			name:             "granite_time_milliseconds_should_be_non_nil_and_fail",
			timeSeconds:      timeSeconds,
			timeMilliseconds: nil,
			extraConfig:      extras.TestGraniteChainConfig,
			expectedErr:      ErrTimeMillisecondsRequired,
		},
		{
			name:             "granite_time_milliseconds_matching_time_should_work",
			timeSeconds:      timeSeconds,
			timeMilliseconds: utils.NewUint64(timeSeconds * 1000),
			extraConfig:      extras.TestGraniteChainConfig,
			expectedErr:      nil,
		},
		{
			name:             "granite_time_milliseconds_matching_time_rounded_should_work",
			timeSeconds:      timeSeconds,
			timeMilliseconds: utils.NewUint64(timeMillis),
			extraConfig:      extras.TestGraniteChainConfig,
			expectedErr:      nil,
		},
		{
			name:             "granite_time_milliseconds_less_than_time_should_fail",
			timeSeconds:      timeSeconds,
			timeMilliseconds: utils.NewUint64(timeSeconds*1000 - 1),
			extraConfig:      extras.TestGraniteChainConfig,
			expectedErr:      ErrTimeMillisecondsMismatched,
		},
		{
			name:             "granite_time_milliseconds_greater_than_time_should_fail",
			timeSeconds:      timeSeconds,
			timeMilliseconds: utils.NewUint64(timeSeconds * 1001),
			extraConfig:      extras.TestGraniteChainConfig,
			expectedErr:      ErrTimeMillisecondsMismatched,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := customtypes.WithHeaderExtra(
				&types.Header{
					Time: test.timeSeconds,
				},
				&customtypes.HeaderExtra{
					TimeMilliseconds: test.timeMilliseconds,
				},
			)
			err := VerifyTime(test.extraConfig, header, time)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
