// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
)

func TestGossipAtomicTxMarshal(t *testing.T) {
	require := require.New(t)

	expected := &GossipAtomicTx{
		Tx: &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{},
			Creds:            []verify.Verifiable{},
		},
	}

	key0 := testKeys[0]
	require.NoError(expected.Tx.Sign(Codec, [][]*secp256k1.PrivateKey{{key0}}))

	bytes, err := expected.Marshal()
	require.NoError(err)

	actual := &GossipAtomicTx{}
	require.NoError(actual.Unmarshal(bytes))

	require.NoError(err)
	require.Equal(expected.GetID(), actual.GetID())
}

func TestAtomicMempoolIterate(t *testing.T) {
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
		name           string
		add            []*GossipAtomicTx
		f              func(tx *GossipAtomicTx) bool
		possibleValues []*GossipAtomicTx
		expectedLen    int
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return false
			},
			possibleValues: nil,
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return true
			},
			possibleValues: txs,
			expectedLen:    2,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			possibleValues: txs,
			expectedLen:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMempool(&snow.Context{}, 10, nil)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*GossipAtomicTx, 0)
			f := func(tx *GossipAtomicTx) bool {
				match := tt.f(tx)

				if match {
					matches = append(matches, tx)
				}

				return match
			}

			m.Iterate(f)

			require.Len(matches, tt.expectedLen)
			require.Subset(tt.possibleValues, matches)
		})
	}
}

func TestGossipEthTxPoolGet(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	signedTx1, err := types.SignTx(
		types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				Value:    big.NewInt(1),
				Gas:      1_000_000,
				GasPrice: big.NewInt(1),
			},
		),
		types.HomesteadSigner{},
		key,
	)
	require.NoError(t, err)

	signedTx2, err := types.SignTx(
		types.NewTx(
			&types.LegacyTx{
				Nonce:    2,
				Value:    big.NewInt(1),
				Gas:      1_000_000,
				GasPrice: big.NewInt(1),
			},
		),
		types.HomesteadSigner{},
		key,
	)
	require.NoError(t, err)

	tx1 := &GossipEthTx{
		Tx: signedTx1,
	}

	tx2 := &GossipEthTx{
		Tx: signedTx2,
	}

	tests := []struct {
		name   string
		add    []*GossipEthTx
		get    *GossipEthTx
		want   *GossipEthTx
		wantOk bool
	}{
		{
			name: "empty",
			get:  tx1,
		},
		{
			name: "does not have tx",
			add:  []*GossipEthTx{tx1},
			get:  tx2,
		},
		{
			name:   "has tx",
			add:    []*GossipEthTx{tx1},
			get:    tx1,
			want:   tx1,
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			engine := dummy.NewFaker()

			genesis := &core.Genesis{
				Config:  params.TestChainConfig,
				Alloc:   core.GenesisAlloc{addr: {Balance: big.NewInt(1_000_000_000)}},
				BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
			}

			chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), core.DefaultCacheConfig, genesis, engine, vm.Config{}, common.Hash{}, false)
			require.NoError(err)

			mempool, err := NewGossipEthTxPool(txpool.NewTxPool(txpool.Config{}, params.TestChainConfig, chain))
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(mempool.Add(add))
			}

			got, ok := mempool.Get(tt.get.GetID())
			require.Equal(tt.wantOk, ok)
			require.Equal(tt.want, got)
		})
	}
}
