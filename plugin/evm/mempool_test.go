// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/stretchr/testify/require"
)

func TestMempoolAddTx(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, 5_000, nil)
	require.NoError(err)

	txs := make([]*GossipAtomicTx, 0)
	for i := 0; i < 3_000; i++ {
		tx := &GossipAtomicTx{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		}

		txs = append(txs, tx)
		require.NoError(m.Add(tx))
	}

	for _, tx := range txs {
		require.True(m.bloom.Has(tx))
	}
}

func TestMempoolGetTx(t *testing.T) {
	tx1 := &GossipAtomicTx{
		Tx: &Tx{
			UnsignedAtomicTx: &TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		},
	}

	tx2 := &GossipAtomicTx{
		Tx: &Tx{
			UnsignedAtomicTx: &TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		},
	}

	tests := []struct {
		name   string
		add    []*GossipAtomicTx
		get    *GossipAtomicTx
		want   *GossipAtomicTx
		wantOk bool
	}{
		{
			name: "empty - does not have",
			get:  tx1,
		},
		{
			name: "populated - does not have",
			add:  []*GossipAtomicTx{tx1},
			get:  tx2,
		},
		{
			name:   "populated - has",
			add:    []*GossipAtomicTx{tx1},
			get:    tx1,
			want:   tx1,
			wantOk: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMempool(&snow.Context{}, 5_000, nil)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			got, ok := m.Get(tt.get.GetID())
			require.Equal(tt.wantOk, ok)
			require.Equal(tt.want, got)
		})
	}
}
