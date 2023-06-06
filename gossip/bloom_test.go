// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	bloomfilter "github.com/holiman/bloomfilter/v2"
	"github.com/stretchr/testify/require"
)

func TestBloomFilterRefresh(t *testing.T) {
	tests := []struct {
		name         string
		refreshRatio float64
		add          []ids.ID
		expected     []ids.ID
	}{
		{
			name:         "no refresh",
			refreshRatio: 1,
			add: []ids.ID{
				{0},
			},
			expected: []ids.ID{
				{0},
			},
		},
		{
			name:         "refresh",
			refreshRatio: 0.1,
			add: []ids.ID{
				{0},
				{1},
			},
			expected: []ids.ID{
				{1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			b, err := bloomfilter.New(10, 1)
			require.NoError(err)

			for _, item := range tt.add {
				ResetBloomFilterIfNeeded(&b, tt.refreshRatio)
				b.Add(hasher{ID: item})
			}

			require.Equal(uint64(len(tt.expected)), b.N())

			for _, expected := range tt.expected {
				require.True(b.Contains(hasher{ID: expected}))
			}
		})
	}
}
