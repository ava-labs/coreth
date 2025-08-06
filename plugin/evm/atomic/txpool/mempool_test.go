// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"fmt"
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

func TestMempoolIterate(t *testing.T) {
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

			var matches []*atomic.Tx
			f := func(tx *atomic.Tx) bool {
				if !tt.f(tx) {
					return false
				}

				matches = append(matches, tx)
				return true
			}

			m.Iterate(f)

			require.ElementsMatch(tt.expectedTxs, matches)
		})
	}
}

func TestMempoolAdd(t *testing.T) {
	tx0 := atomictest.GenerateTestImportTxWithGas(1, 1)
	tx1 := atomictest.GenerateTestImportTxWithGas(1, 1)
	tx2 := atomictest.GenerateTestImportTxWithGas(1, 2)

	almostMaxFeeTx := atomictest.GenerateTestImportTxWithGas(1, math.MaxUint64-1)
	maxFeeTx := atomictest.GenerateTestImportTxWithGas(1, math.MaxUint64)

	tests := []struct {
		name         string
		size         int
		issuedTxs    []*atomic.Tx
		discardedTxs []*atomic.Tx
		currentTxs   []*atomic.Tx
		pendingTxs   []*atomic.Tx

		tx      *atomic.Tx
		local   bool
		wantErr error
	}{
		{
			name: "lower_fee_not_added",
			size: 1,
			pendingTxs: []*atomic.Tx{
				tx2,
			},
			tx:      tx0,
			wantErr: ErrInsufficientFee,
		},
		{
			name: "same_fee_not_added",
			size: 1,
			pendingTxs: []*atomic.Tx{
				tx0,
			},
			tx:      tx1,
			wantErr: ErrInsufficientFee,
		},
		{
			name: "lower_fee_dropped",
			size: 1,
			pendingTxs: []*atomic.Tx{
				tx0,
			},
			tx: tx2,
		},
		{
			name: "mempool_full",
			size: 1,
			issuedTxs: []*atomic.Tx{
				tx0,
			},
			tx:      maxFeeTx,
			wantErr: ErrMempoolFull,
		},
		{
			name: "duplicate_issued",
			size: 1,
			issuedTxs: []*atomic.Tx{
				tx0,
			},
			tx:      tx0,
			wantErr: ErrAlreadyKnown,
		},
		{
			name: "duplicate_current",
			size: 1,
			currentTxs: []*atomic.Tx{
				tx0,
			},
			tx:      tx0,
			wantErr: ErrAlreadyKnown,
		},
		{
			name: "duplicate_pending",
			size: 1,
			pendingTxs: []*atomic.Tx{
				tx0,
			},
			tx:      tx0,
			wantErr: ErrAlreadyKnown,
		},
		{
			name: "cancelled_txs_in_bloom_filter",
			size: 2,
			currentTxs: []*atomic.Tx{
				maxFeeTx,
			},
			pendingTxs: func() []*atomic.Tx {
				numHashes, numEntries := bloom.OptimalParameters(
					config.TxGossipBloomMinTargetElements,
					config.TxGossipBloomTargetFalsePositiveRate,
				)
				txsToAdd := bloom.EstimateCount(
					numHashes,
					numEntries,
					config.TxGossipBloomResetFalsePositiveRate,
				)
				// Reduce txsToAdd by one to account for the manually added tx.
				txs := make([]*atomic.Tx, txsToAdd-1)
				for i := range txs {
					// Keep increasing the fee to cause prior transactions to be
					// evicted.
					fee := uint64(i + 1)
					txs[i] = atomictest.GenerateTestImportTxWithGas(1, fee)
				}
				return txs
			}(),
			tx: almostMaxFeeTx,
		},
		{
			name: "discarded_tx_not_verified",
			size: 1,
			discardedTxs: []*atomic.Tx{
				tx0,
			},
			tx:      tx0,
			wantErr: ErrAlreadyKnown,
		},
		{
			name: "discarded_tx_verified",
			size: 1,
			discardedTxs: []*atomic.Tx{
				tx0,
			},
			tx:    tx0,
			local: true,
		},
	}
	for _, test := range tests {
		addRemotes := []func(*Mempool, *atomic.Tx) error{
			(*Mempool).Add,
			(*Mempool).AddRemoteTx,
		}
		for i, addRemote := range addRemotes {
			t.Run(fmt.Sprintf("%s_%d", test.name, i), func(t *testing.T) {
				require := require.New(t)

				ctx := snowtest.Context(t, snowtest.CChainID)
				mempool, err := NewMempool(
					NewTxs(ctx, test.size),
					prometheus.NewRegistry(),
					nil,
				)
				require.NoError(err)

				// Add all transactions that should be in the Issued state.
				for _, tx := range test.issuedTxs {
					require.NoError(mempool.AddRemoteTx(tx))
					require.True(mempool.Has(tx.ID()))
				}
				makePendingCurrent(mempool)
				mempool.IssueCurrentTxs()

				// Add all transactions that should be in the Discarded state.
				for _, tx := range test.discardedTxs {
					require.NoError(mempool.AddRemoteTx(tx))
					require.True(mempool.Has(tx.ID()))
				}
				makePendingCurrent(mempool)
				mempool.DiscardCurrentTxs()

				// Add all transactions that should be in the Current state.
				for _, tx := range test.currentTxs {
					require.NoError(mempool.AddRemoteTx(tx))
					require.True(mempool.Has(tx.ID()))
				}
				makePendingCurrent(mempool)

				// Add all transactions that should be in the Pending state.
				for _, tx := range test.pendingTxs {
					require.NoError(mempool.AddRemoteTx(tx))
					require.True(mempool.Has(tx.ID()))
				}

				// Attempt to add the transaction under test
				if test.local {
					err = mempool.AddLocalTx(test.tx)
				} else {
					err = addRemote(mempool, test.tx)
				}
				require.ErrorIs(err, test.wantErr)
				if err == nil {
					require.True(mempool.Has(test.tx.ID()))
				}

				assertInvariants(t, mempool)

				// This was added as a regression test for inconsistent bloom
				// filter handling of cancelled transactions after successful
				// transaction additions.
				mempool.CancelCurrentTxs()
				assertInvariants(t, mempool)
			})
		}
	}
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

// makePendingCurrent marks all pending transactions as current.
func makePendingCurrent(mempool *Mempool) {
	for {
		_, ok := mempool.NextTx() // Move all txs to current
		if !ok {
			break
		}
	}
}
