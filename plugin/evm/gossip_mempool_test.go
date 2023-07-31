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

//
// func TestEthTxPoolAddTx(t *testing.T) {
// 	key, err := crypto.GenerateKey()
// 	require.NoError(t, err)
//
// 	txs := []*types.Transaction{
// 		types.NewTx(&types.AccessListTx{
// 			ChainID:  params.TestChainConfig.ChainID,
// 			Nonce:    0,
// 			GasPrice: big.NewInt(1),
// 			Gas:      100_000,
// 			To:       &common.Address{},
// 			Value:    big.NewInt(0),
// 			Data:     []byte{},
// 			V:        big.NewInt(32),
// 			R:        big.NewInt(10),
// 			S:        big.NewInt(11),
// 		}),
// 		types.NewTx(&types.AccessListTx{
// 			ChainID:  params.TestChainConfig.ChainID,
// 			Nonce:    1,
// 			GasPrice: big.NewInt(1),
// 			Gas:      100_000,
// 			To:       &common.Address{},
// 			Value:    big.NewInt(0),
// 			Data:     []byte{},
// 			V:        big.NewInt(32),
// 			R:        big.NewInt(10),
// 			S:        big.NewInt(11),
// 		}),
// 	}
//
// 	signedTxs := make([]*GossipEthTx, 0, len(txs))
// 	for _, tx := range txs {
// 		signedTx, err := types.SignTx(tx, types.LatestSigner(params.TestChainConfig), key)
// 		require.NoError(t, err)
// 		signedTxs = append(signedTxs, &GossipEthTx{
// 			Tx: signedTx,
// 		})
// 	}
//
// 	tests := []struct {
// 		name     string
// 		add      []*GossipEthTx
// 		filter   func(tx *GossipEthTx) bool
// 		expected []*GossipEthTx
// 	}{
// 		{
// 			name: "empty",
// 		},
// 		{
// 			name: "filter matches nothing",
// 			add:  signedTxs,
// 			filter: func(tx *GossipEthTx) bool {
// 				return false
// 			},
// 			expected: nil,
// 		},
// 		{
// 			name: "filter matches all",
// 			add:  signedTxs,
// 			filter: func(*GossipEthTx) bool {
// 				return true
// 			},
// 			expected: signedTxs,
// 		},
// 		{
// 			name: "filter matches subset",
// 			add:  signedTxs,
// 			filter: func(tx *GossipEthTx) bool {
// 				return tx.Tx == signedTxs[0].Tx
// 			},
// 			expected: signedTxs[:1],
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			require := require.New(t)
//
// 			config := txpool.DefaultConfig
// 			config.Journal = ""
// 			m := txpool.NewTestTxPool(config, params.TestChainConfig, 100_000_000, key.PublicKey)
// 			pending := make(chan core.NewTxsEvent)
// 			m.SubscribeNewTxsEvent(pending)
// 			mempool, err := NewGossipEthTxPool(m)
// 			require.NoError(err)
//
// 			for _, add := range tt.add {
// 				ok, err := mempool.AddTx(add, false)
// 				require.True(ok)
// 				require.NoError(err)
// 				<-pending
// 			}
//
// 			txs := mempool.Get(tt.filter)
// 			require.Len(txs, len(tt.expected))
//
// 			for _, expected := range tt.expected {
// 				require.Contains(txs, expected)
// 			}
// 		})
// 	}
// }
