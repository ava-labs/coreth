// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestMempoolAddTx(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, prometheus.NewRegistry(), 5_000, nil)
	require.NoError(err)

	txs := make([]*atomic.GossipAtomicTx, 0)
	for i := 0; i < 3_000; i++ {
		tx := &atomic.GossipAtomicTx{
			Tx: &atomic.Tx{
				UnsignedAtomicTx: &atomictest.TestUnsignedTx{
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

// Add should return an error if a tx is already known
func TestMempoolAdd(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, prometheus.NewRegistry(), 5_000, nil)
	require.NoError(err)

	tx := &atomic.GossipAtomicTx{
		Tx: &atomic.Tx{
			UnsignedAtomicTx: &atomictest.TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		},
	}

	require.NoError(m.Add(tx))
	err = m.Add(tx)
	require.ErrorIs(err, errTxAlreadyKnown)
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*atomic.GossipAtomicTx{
		{
			Tx: &atomic.Tx{
				UnsignedAtomicTx: &atomictest.TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
		{
			Tx: &atomic.Tx{
				UnsignedAtomicTx: &atomictest.TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
	}

	tests := []struct {
		name        string
		add         []*atomic.GossipAtomicTx
		f           func(tx *atomic.GossipAtomicTx) bool
		expectedTxs []*atomic.GossipAtomicTx
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*atomic.GossipAtomicTx) bool {
				return false
			},
			expectedTxs: []*atomic.GossipAtomicTx{},
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*atomic.GossipAtomicTx) bool {
				return true
			},
			expectedTxs: txs,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *atomic.GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			expectedTxs: []*atomic.GossipAtomicTx{txs[0]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMempool(&snow.Context{}, prometheus.NewRegistry(), 10, nil)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*atomic.GossipAtomicTx, 0)
			f := func(tx *atomic.GossipAtomicTx) bool {
				match := tt.f(tx)

				if match {
					matches = append(matches, tx)
				}

				return match
			}

			m.Iterate(f)

			require.ElementsMatch(tt.expectedTxs, matches)
		})
	}
}
