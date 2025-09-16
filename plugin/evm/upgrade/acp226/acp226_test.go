// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp226

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	readerTests = []struct {
		name                        string
		state                       State
		skipTestDesiredTargetExcess bool
		delay                       uint64
	}{
		{
			name: "zero",
			state: State{
				TargetExcess: 0,
			},
			delay: MinTargetDelayMilliseconds,
		},
		{
			name: "small_excess_change",
			state: State{
				TargetExcess: 726820, // Smallest excess that increases the target
			},
			delay: MinTargetDelayMilliseconds + 1,
		},
		{
			name: "max_initial_excess_change",
			state: State{
				TargetExcess: MaxTargetExcessDiff,
			},
			skipTestDesiredTargetExcess: true,
			delay:                       1,
		},
		{
			name: "100ms_target",
			state: State{
				TargetExcess: 4_828_872, // TargetConversion (2^20) * ln(100) + 2
			},
			delay: 100,
		},
		{
			name: "500ms_target",
			state: State{
				TargetExcess: 6_516_490, // TargetConversion (2^20) * ln(500) + 2
			},
			delay: 500,
		},
		{
			name: "1000ms_target",
			state: State{
				TargetExcess: 7_243_307, // TargetConversion (2^20) * ln(1000) + 1
			},
			delay: 1000,
		},
		{
			name: "2000ms_target",
			state: State{
				TargetExcess: 7_970_124, // TargetConversion (2^20) * ln(2000) + 1
			},
			delay: 2000,
		},
		{
			name: "5000ms_target",
			state: State{
				TargetExcess: 8_930_925, // TargetConversion (2^20) * ln(5000) + 1
			},
			delay: 5000,
		},
		{
			name: "10000ms_target",
			state: State{
				TargetExcess: 9_657_742, // TargetConversion (2^20) * ln(10000) + 1
			},
			delay: 10000,
		},
		{
			name: "60000ms_target",
			state: State{
				TargetExcess: 11_536_538, // TargetConversion (2^20) * ln(60000) + 1
			},
			delay: 60000,
		},
		{
			name: "300000ms_target",
			state: State{
				TargetExcess: 13_224_156, // TargetConversion (2^20) * ln(300000) + 1
			},
			delay: 300000,
		},
		{
			name: "largest_int64_target",
			state: State{
				TargetExcess: 45_789_502, // TargetConversion (2^20) * ln(MaxInt64)
			},
			delay: 9_223_368_741_047_657_702,
		},
		{
			name: "second_largest_uint64_target",
			state: State{
				TargetExcess: maxTargetExcess - 1,
			},
			delay: 18_446_728_723_565_431_225,
		},
		{
			name: "largest_uint64_target",
			state: State{
				TargetExcess: maxTargetExcess,
			},
			delay: math.MaxUint64,
		},
		{
			name: "largest_excess",
			state: State{
				TargetExcess: math.MaxUint64,
			},
			skipTestDesiredTargetExcess: true,
			delay:                       math.MaxUint64,
		},
	}
	updateTargetExcessTests = []struct {
		name                string
		initial             State
		desiredTargetExcess uint64
		expected            State
	}{
		{
			name: "no_change",
			initial: State{
				TargetExcess: 0,
			},
			desiredTargetExcess: 0,
			expected: State{
				TargetExcess: 0,
			},
		},
		{
			name: "max_increase",
			initial: State{
				TargetExcess: 0,
			},
			desiredTargetExcess: MaxTargetExcessDiff + 1,
			expected: State{
				TargetExcess: MaxTargetExcessDiff, // capped
			},
		},
		{
			name: "inverse_max_increase",
			initial: State{
				TargetExcess: MaxTargetExcessDiff,
			},
			desiredTargetExcess: 0,
			expected: State{
				TargetExcess: 0,
			},
		},
		{
			name: "max_decrease",
			initial: State{
				TargetExcess: 2 * MaxTargetExcessDiff,
			},
			desiredTargetExcess: 0,
			expected: State{
				TargetExcess: MaxTargetExcessDiff,
			},
		},
		{
			name: "inverse_max_decrease",
			initial: State{
				TargetExcess: MaxTargetExcessDiff,
			},
			desiredTargetExcess: 2 * MaxTargetExcessDiff,
			expected: State{
				TargetExcess: 2 * MaxTargetExcessDiff,
			},
		},
		{
			name: "small_increase",
			initial: State{
				TargetExcess: 50,
			},
			desiredTargetExcess: 100,
			expected: State{
				TargetExcess: 100,
			},
		},
		{
			name: "small_decrease",
			initial: State{
				TargetExcess: 100,
			},
			desiredTargetExcess: 50,
			expected: State{
				TargetExcess: 50,
			},
		},
		{
			name: "large_increase_capped",
			initial: State{
				TargetExcess: 0,
			},
			desiredTargetExcess: 1000,
			expected: State{
				TargetExcess: MaxTargetExcessDiff, // capped at 200
			},
		},
		{
			name: "large_decrease_capped",
			initial: State{
				TargetExcess: 1000,
			},
			desiredTargetExcess: 0,
			expected: State{
				TargetExcess: 1000 - MaxTargetExcessDiff, // 800
			},
		},
	}
	parseTests = []struct {
		name        string
		bytes       []byte
		state       State
		expectedErr error
	}{
		{
			name:        "insufficient_length",
			bytes:       make([]byte, StateSize-1),
			expectedErr: ErrStateInsufficientLength,
		},
		{
			name:  "zero_state",
			bytes: make([]byte, StateSize),
			state: State{},
		},
		{
			name: "truncate_bytes",
			bytes: []byte{
				StateSize: 1,
			},
			state: State{},
		},
		{
			name: "endianness",
			bytes: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			},
			state: State{
				TargetExcess: 0x0102030405060708,
			},
		},
		{
			name: "max_uint64",
			bytes: []byte{
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			},
			state: State{
				TargetExcess: math.MaxUint64,
			},
		},
		{
			name: "min_uint64",
			bytes: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			state: State{
				TargetExcess: 0,
			},
		},
	}
)

func TestTargetDelay(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.delay, test.state.TargetDelay())
		})
	}
}

func BenchmarkTargetDelay(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.state.TargetDelay()
			}
		})
	}
}

func TestUpdateTargetExcess(t *testing.T) {
	for _, test := range updateTargetExcessTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.UpdateTargetExcess(test.desiredTargetExcess)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkUpdateTargetExcess(b *testing.B) {
	for _, test := range updateTargetExcessTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.UpdateTargetExcess(test.desiredTargetExcess)
			}
		})
	}
}

func TestDesiredTargetExcess(t *testing.T) {
	for _, test := range readerTests {
		if test.skipTestDesiredTargetExcess {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.state.TargetExcess, DesiredTargetExcess(test.delay))
		})
	}
}

func BenchmarkDesiredTargetExcess(b *testing.B) {
	for _, test := range readerTests {
		if test.skipTestDesiredTargetExcess {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				DesiredTargetExcess(test.delay)
			}
		})
	}
}

func TestParseMinimumBlockDelayExcess(t *testing.T) {
	for _, test := range parseTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state, err := ParseMinimumBlockDelayExcess(test.bytes)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.state, state)
		})
	}
}

func BenchmarkParseMinimumBlockDelayExcess(b *testing.B) {
	for _, test := range parseTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_, _ = ParseMinimumBlockDelayExcess(test.bytes)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	for _, test := range parseTests {
		if test.expectedErr != nil {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			expectedBytes := test.bytes[:StateSize]
			bytes := test.state.Bytes()
			require.Equal(t, expectedBytes, bytes)
		})
	}
}

func BenchmarkBytes(b *testing.B) {
	for _, test := range parseTests {
		if test.expectedErr != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_ = test.state.Bytes()
			}
		})
	}
}
