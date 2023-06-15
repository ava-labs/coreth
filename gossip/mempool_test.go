// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_BloomFilterAdd(t *testing.T) {
	tests := []struct {
		name     string
		add      [][]byte
		expected [][]byte
	}{
		{
			name: "add single element",
			add: [][]byte{
				[]byte("00000000"),
			},
			expected: [][]byte{
				[]byte("00000000"),
			},
		},
		{
			name: "refresh filter",
			add: [][]byte{
				[]byte("00000000"),
				[]byte("00000001"),
			},
			expected: [][]byte{
				[]byte("00000001"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			b, err := newBloomFilter(10, 1, 0.1)
			r.NoError(err)

			for _, item := range tt.add {
				b.Add(item)
			}

			r.Equal(b.bloomFilter.N(), uint64(len(tt.expected)))

			for _, expected := range tt.expected {
				r.True(b.Contains(expected))
			}
		})

	}
}

func TestBloomFilter_MarshalBinary(t *testing.T) {
	r := require.New(t)

	original, err := NewBloomFilter()
	r.NoError(err)

	bytes, err := original.MarshalBinary()
	r.NoError(err)

	unmarshalled, err := NewBloomFilter()
	r.NoError(err)
	r.NoError(unmarshalled.UnmarshalBinary(bytes))

	r.Equal(original, unmarshalled)
}
