// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackUnpackHeights(t *testing.T) {
	require := require.New(t)
	tests := [][]uint64{
		[]uint64{0, 1, 2, 3, 4},
		[]uint64{},
		[]uint64{1},
	}

	for _, test := range tests {
		heights := test
		b, err := packHeightsSlice(heights)
		require.NoError(err)

		res, err := unpackHeightsSlice(b)
		require.NoError(err)
		require.Equal(heights, res)
	}
}
