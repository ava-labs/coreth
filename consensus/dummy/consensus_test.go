// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/params/extras/extrastest"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
)

func TestVerifyBlockFee(t *testing.T) {
	tests := map[string]struct {
		baseFee                *big.Int
		parentBlockGasCost     *big.Int
		timeElapsed            uint64
		txs                    []*types.Transaction
		receipts               []*types.Receipt
		extraStateContribution *big.Int
		rules                  extras.AvalancheRules
		overrideBlockGasCost   *big.Int
		expectedErrMsg         string
	}{
		"tx only base fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			expectedErrMsg:         "insufficient gas to cover the block cost: expected 100000 but got 0",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"tx covers exactly block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(200), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
			},
			extraStateContribution: nil,
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"txs share block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(200), nil),
				types.NewTransaction(1, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
				{GasUsed: 100_000},
			},
			extraStateContribution: nil,
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"txs split block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(150), nil),
				types.NewTransaction(1, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
				{GasUsed: 100_000},
			},
			extraStateContribution: nil,
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"split block fee with extra state contribution": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
			},
			extraStateContribution: big.NewInt(5_000_000),
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"extra state contribution insufficient": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(9_999_999),
			expectedErrMsg:         "insufficient gas to cover the block cost: expected 100000 but got 99999",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"negative extra state contribution": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(-1),
			expectedErrMsg:         "invalid extra state change contribution: -1",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"extra state contribution covers block fee": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(10_000_000),
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"extra state contribution covers more than block fee": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(10_000_001),
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"tx only base fee after full time window": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(500_000),
			timeElapsed:        ap4.TargetBlockRate + 10,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"tx only base fee after large time window": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(100_000),
			timeElapsed:        math.MaxUint64,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.ApricotPhase4),
		},
		"fortuna minimum base fee should fail": {
			baseFee:                big.NewInt(1),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: nil,
			expectedErrMsg:         "insufficient gas to cover the block cost: expected 400000 but got 0",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.Fortuna),
		},
		"granite minimum base fee should not fail ": {
			baseFee:                big.NewInt(1),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: nil,
			expectedErrMsg:         "",
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
		},
		"granite block gas cost = 0 should not fail": {
			baseFee:                big.NewInt(1),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: nil,
			expectedErrMsg:         "",
			overrideBlockGasCost:   big.NewInt(0),
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
		},
		"granite block gas cost > 0 should fail": {
			baseFee:                big.NewInt(1),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: nil,
			expectedErrMsg:         errInvalidBlockGasCostGranite.Error(),
			overrideBlockGasCost:   big.NewInt(1),
			rules:                  extrastest.GetAvalancheRulesFromFork(upgradetest.Granite),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			parent := customtypes.WithHeaderExtra(
				&types.Header{
					Time: 0, // uses timeElapsed to calculate block gas cost
				},
				&customtypes.HeaderExtra{
					BlockGasCost: test.parentBlockGasCost,
				},
			)
			blockGasCost := test.overrideBlockGasCost
			if blockGasCost == nil {
				blockGasCost = header.BlockGasCost(
					test.rules,
					parent,
					test.timeElapsed,
				)
			}

			engine := NewFaker()
			err := engine.verifyBlockFee(test.baseFee, blockGasCost, test.txs, test.receipts, test.extraStateContribution, test.rules)
			if test.expectedErrMsg == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.expectedErrMsg)
			}
		})
	}
}
