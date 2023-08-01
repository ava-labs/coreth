// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAtomicMempoolAddTx(t *testing.T) {
	txs := []*GossipAtomicTx{
		{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
		{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
	}

	tests := []struct {
		name     string
		add      []*GossipAtomicTx
		filter   func(tx *GossipAtomicTx) bool
		expected []*GossipAtomicTx
	}{
		{
			name: "empty",
		},
		{
			name: "filter matches nothing",
			add:  txs,
			filter: func(*GossipAtomicTx) bool {
				return false
			},
			expected: nil,
		},
		{
			name: "filter matches all",
			add:  txs,
			filter: func(*GossipAtomicTx) bool {
				return true
			},
			expected: txs,
		},
		{
			name: "filter matches subset",
			add:  txs,
			filter: func(tx *GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			expected: txs[:1],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m := NewMempool(ids.Empty, 10)
			mempool, err := NewGossipAtomicMempool(m)
			require.NoError(err)

			for _, add := range tt.add {
				ok, err := mempool.Add(add)
				require.True(ok)
				require.NoError(err)
			}

			txs := mempool.Get(tt.filter)
			require.Len(txs, len(tt.expected))

			for _, expected := range tt.expected {
				require.Contains(txs, expected)
			}
		})
	}
}
