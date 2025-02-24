// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/stretchr/testify/require"
)

func TestParseFeeState(t *testing.T) {
	tests := []struct {
		name     string
		bytes    []byte
		state    acp176.State
		parseErr error
	}{
		{
			name:     "insufficient_length",
			bytes:    make([]byte, FeeStateSize-1),
			parseErr: errFeeStateInsufficientLength,
		},
		{
			name:  "zero_window",
			bytes: make([]byte, FeeWindowSize),
			state: acp176.State{},
		},
		{
			name: "truncate_bytes",
			bytes: []byte{
				FeeStateSize: 1,
			},
			state: acp176.State{},
		},
		{
			name: "endianess",
			bytes: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
			},
			state: acp176.State{
				Gas: gas.State{
					Capacity: 0x0102030405060708,
					Excess:   0x1112131415161718,
				},
				TargetExcess: 0x2122232425262728,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state, err := parseFeeState(test.bytes)
			require.Equal(test.state, state)
			require.ErrorIs(err, test.parseErr)
			if test.parseErr != nil {
				return
			}

			expectedBytes := test.bytes[:FeeStateSize]
			bytes := feeStateBytes(state)
			require.Equal(expectedBytes, bytes)
		})
	}
}
