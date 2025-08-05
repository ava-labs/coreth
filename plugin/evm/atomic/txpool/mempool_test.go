// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMempoolAddTx(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(
		NewTxs(ctx, 5_000),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	txs := make([]*atomic.Tx, 0)
	for i := 0; i < 3_000; i++ {
		tx := atomictest.GenerateTestImportTxWithGas(1, 1)
		txs = append(txs, tx)
		require.NoError(m.Add(tx))
	}

	assertInvariants(t, m)
}

// Add should return an error if a tx is already known
func TestMempoolAdd(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(
		NewTxs(ctx, 5_000),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx := atomictest.GenerateTestImportTxWithGas(1, 1)
	require.NoError(m.Add(tx))
	err = m.Add(tx)
	require.ErrorIs(err, ErrAlreadyKnown)
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*atomic.Tx{
		atomictest.GenerateTestImportTxWithGas(1, 1),
		atomictest.GenerateTestImportTxWithGas(1, 1),
	}

	tests := []struct {
		name        string
		add         []*atomic.Tx
		f           func(tx *atomic.Tx) bool
		expectedTxs []*atomic.Tx
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*atomic.Tx) bool {
				return false
			},
			expectedTxs: []*atomic.Tx{},
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*atomic.Tx) bool {
				return true
			},
			expectedTxs: txs,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *atomic.Tx) bool {
				return tx == txs[0]
			},
			expectedTxs: []*atomic.Tx{txs[0]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := snowtest.Context(t, snowtest.CChainID)
			m, err := NewMempool(
				NewTxs(ctx, 10),
				prometheus.NewRegistry(),
				nil,
			)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*atomic.Tx, 0)
			f := func(tx *atomic.Tx) bool {
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

// a valid tx shouldn't be added to the mempool if this would exceed the
// mempool's max size
func TestMempoolMaxSizeHandling(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(
		NewTxs(ctx, 1),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)
	// create candidate tx (we will drop before validation)
	tx := atomictest.GenerateTestImportTxWithGas(1, 1)

	require.NoError(mempool.AddRemoteTx(tx))
	require.True(mempool.Has(tx.ID()))
	// promote tx to be issued
	_, ok := mempool.NextTx()
	require.True(ok)
	mempool.IssueCurrentTxs()

	// try to add one more tx
	tx2 := atomictest.GenerateTestImportTxWithGas(1, 1)
	err = mempool.AddRemoteTx(tx2)
	require.ErrorIs(err, ErrMempoolFull)
	require.False(mempool.Has(tx2.ID()))
}

// mempool will drop transaction with the lowest fee
func TestMempoolPriorityDrop(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(
		NewTxs(ctx, 1),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx1 := atomictest.GenerateTestImportTxWithGas(1, 2) // lower fee
	require.NoError(mempool.AddRemoteTx(tx1))
	require.True(mempool.Has(tx1.ID()))

	tx2 := atomictest.GenerateTestImportTxWithGas(1, 2) // lower fee
	err = mempool.AddRemoteTx(tx2)
	require.ErrorIs(err, ErrInsufficientFee)
	require.True(mempool.Has(tx1.ID()))
	require.False(mempool.Has(tx2.ID()))

	tx3 := atomictest.GenerateTestImportTxWithGas(1, 5) // higher fee
	require.NoError(mempool.AddRemoteTx(tx3))
	require.False(mempool.Has(tx1.ID()))
	require.False(mempool.Has(tx2.ID()))
	require.True(mempool.Has(tx3.ID()))
}

func assertInvariants(t *testing.T, m *Mempool) {
	assert := assert.New(t)

	// Assert that a transaction is only in one state at a time, either pending,
	// current, issued, or discarded.
	{
		var txIDs bag.Bag[ids.ID]
		for _, tx := range m.pendingTxs.maxHeap.items {
			txIDs.Add(tx.id)
		}
		for txID := range m.currentTxs {
			txIDs.Add(txID)
		}
		for txID := range m.issuedTxs {
			txIDs.Add(txID)
		}
		txID, freq := txIDs.Mode()
		assert.LessOrEqual(freq, 1, "%q included multiple times", txID)

		for _, txID := range txIDs.List() {
			_, discarded := m.discardedTxs.Get(txID)
			assert.Falsef(discarded, "%q included multiple times", txID)
		}
	}

	// Assert that all the UTXOs currently referenced by a tx in the mempool is
	// tracked in the utxo spenders map.
	{
		var utxoIDs set.Set[ids.ID]
		for _, tx := range m.pendingTxs.maxHeap.items {
			utxoIDs.Union(tx.tx.InputUTXOs())
		}
		for _, tx := range m.currentTxs {
			utxoIDs.Union(tx.InputUTXOs())
		}
		for _, tx := range m.issuedTxs {
			utxoIDs.Union(tx.InputUTXOs())
		}
		assert.Len(m.utxoSpenders, utxoIDs.Len())
	}

	// Assert that all the pending transactions are included in the bloom
	// filter.
	for _, pendingTx := range m.pendingTxs.maxHeap.items {
		assert.Truef(m.bloom.Has(pendingTx.tx), "%q should have been included in the bloom filter", pendingTx.id)
	}
}

// TestCancelCurrentTx verifies that the mempool invariants are maintained after
// cancelling a current transaction.
//
// This was added as a regression test for inconsistent bloom filter handling.
func TestCancelCurrentTx(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(
		NewTxs(ctx, 2),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	maxFeeTx := atomictest.GenerateTestImportTxWithGas(1, math.MaxUint64) // max fee
	require.NoError(mempool.AddRemoteTx(maxFeeTx))

	txToIssue, ok := mempool.NextTx()
	require.True(ok)
	require.Equal(maxFeeTx, txToIssue)

	numHashes, numEntries := bloom.OptimalParameters(
		config.TxGossipBloomMinTargetElements,
		config.TxGossipBloomTargetFalsePositiveRate,
	)
	txsToAdd := bloom.EstimateCount(
		numHashes,
		numEntries,
		config.TxGossipBloomResetFalsePositiveRate,
	)
	t.Logf("adding %d txs to force the bloom filter to reset", txsToAdd)
	for fee := range txsToAdd {
		// Keep increasing the fee to cause prior transactions to be evicted.
		tx := atomictest.GenerateTestImportTxWithGas(1, uint64(fee+1))
		require.NoError(mempool.AddRemoteTx(tx))
	}

	mempool.CancelCurrentTx(maxFeeTx.ID())
	assertInvariants(t, mempool)
}
