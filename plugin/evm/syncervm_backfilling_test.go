// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackUnpackHeights(t *testing.T) {
	require := require.New(t)
	tests := [][]heightInterval{
		[]heightInterval{
			heightInterval{
				upperBound: 10,
				lowerBound: 2,
			},
			heightInterval{
				upperBound: 100,
				lowerBound: 30,
			},
		},
		[]heightInterval{},
	}

	for _, test := range tests {
		heights := test
		b, err := packHeightIntervals(heights)
		require.NoError(err)

		res, err := unpackHeightIntervals(b)
		require.NoError(err)
		require.Equal(heights, res)
	}
}
