// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyBlockStandalone(t *testing.T) {
	latestConfig := forkToChainConfig[upgradetest.Latest]
	signer := types.LatestSigner(latestConfig)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	tx := types.NewTx(&types.DynamicFeeTx{})
	tx, err = types.SignTx(tx, signer, key)
	require.NoError(t, err)

	var (
		validHeader = customtypes.WithHeaderExtra(
			&types.Header{
				Coinbase:         constants.BlackholeAddr,
				Difficulty:       common.Big1,
				Number:           common.Big1,
				Time:             uint64(upgrade.InitiallyActiveTime.Unix()),
				Extra:            make([]byte, acp176.StateSize),
				BaseFee:          common.Big1,
				BlobGasUsed:      utils.NewUint64(0),
				ExcessBlobGas:    utils.NewUint64(0),
				ParentBeaconRoot: &common.Hash{},
			},
			&customtypes.HeaderExtra{
				ExtDataHash:    customtypes.CalcExtDataHash(nil),
				ExtDataGasUsed: common.Big0,
				BlockGasCost:   common.Big0,
			},
		)
		txs = []*types.Transaction{
			tx,
		}
	)

	tests := []struct {
		name          string
		extDataHashes map[common.Hash]common.Hash
		clock         mockable.Clock
		fork          upgradetest.Fork
		header        *types.Header
		txs           []*types.Transaction
		uncles        []*types.Header
		override      func(*types.Block)
		atomicTxs     []*atomic.Tx
		want          error
	}{
		{
			name:   "valid_latest",
			fork:   upgradetest.Latest,
			header: validHeader,
			txs:    txs,
		},
		{
			name:   "has_uncles",
			fork:   upgradetest.Latest,
			header: validHeader,
			txs:    txs,
			uncles: []*types.Header{validHeader},
			want:   errUnclesUnsupported,
		},
		{
			name: "invalid_coinbase",
			fork: upgradetest.Latest,
			header: func() *types.Header {
				h := types.CopyHeader(validHeader)
				h.Coinbase[0]++
				return h
			}(),
			txs:  txs,
			want: errInvalidCoinbase,
		},
		{
			name: "invalid_difficulty",
			fork: upgradetest.Latest,
			header: func() *types.Header {
				h := types.CopyHeader(validHeader)
				h.Difficulty = common.Big0
				return h
			}(),
			txs:  txs,
			want: errInvalidDifficulty,
		},
		{
			name: "invalid_number_overflow",
			fork: upgradetest.Latest,
			header: func() *types.Header {
				h := types.CopyHeader(validHeader)
				h.Number = new(big.Int).Lsh(common.Big1, 64)
				return h
			}(),
			txs:  txs,
			want: errInvalidNumber,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := forkToChainConfig[test.fork]
			block := types.NewBlock(
				test.header,
				test.txs,
				test.uncles,
				nil,
				trie.NewStackTrie(nil),
			)
			got := verifyBlockStandalone(
				test.extDataHashes,
				&test.clock,
				config,
				block,
				test.atomicTxs,
			)
			assert.ErrorIs(t, got, test.want)
		})
	}
}
