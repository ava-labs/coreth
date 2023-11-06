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

func TestMempoolRemoveTx(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, 5_000, nil)
	require.NoError(err)

	tx := newTestTx()

	err = m.AddTx(tx)
	require.NoError(err)
	require.True(m.has(tx.ID()))

	m.RemoveTx(tx)
	require.False(m.has(tx.ID()))
}

func TestMempoolRemoveTxs(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, 5_000, nil)
	require.NoError(err)

	txs := newTestTxs(1000)
	for _, tx := range txs {
		err := m.AddTx(tx)
		require.NoError(err)
		require.True(m.has(tx.ID()))
	}

	require.Equal(1000, m.length())

	m.RemoveTxs()
	require.Zero(m.length())
	for _, tx := range txs {
		require.False(m.has(tx.ID()))
	}
}

func TestMempoolGetTx(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, 5_000, nil)
	require.NoError(err)

	tx := newTestTx()

	err = m.AddTx(tx)
	require.NoError(err)
	require.True(m.has(tx.ID()))

	fetchedTx, isPending := m.GetTx(tx.ID())
	require.True(isPending)
	require.Equal(tx, fetchedTx)

	otherTx := newTestTx()
	fetchedOtherTx, isPending := m.GetTx(otherTx.ID())
	require.False(isPending)
	require.Nil(fetchedOtherTx)
}

func TestMempoolGetTxs(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(&snow.Context{}, 5_000, nil)
	require.NoError(err)

	tx := newTestTx()

	err = m.AddTx(tx)
	require.NoError(err)
	require.True(m.has(tx.ID()))

	txs := m.GetTxs()
	require.Len(txs, 1)
	require.Equal(tx, txs[0])

	m.RemoveTx(tx)
	require.Equal(0, m.length())

	txs = newTestTxs(1000)
	for _, tx := range txs {
		err := m.AddTx(tx)
		require.NoError(err)
		require.True(m.has(tx.ID()))
	}

	require.Equal(1000, m.length())
	fetchedTxs := m.GetTxs()
	require.Len(fetchedTxs, 1000)
	require.ElementsMatch(txs, fetchedTxs)
}
