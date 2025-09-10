// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/paramstest"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

// dummyChain implements consensus.ChainHeaderReader for testing.
type dummyChain struct{ cfg *params.ChainConfig }

func (m *dummyChain) Config() *params.ChainConfig               { return m.cfg }
func (*dummyChain) CurrentHeader() *types.Header                { return nil }
func (*dummyChain) GetHeader(common.Hash, uint64) *types.Header { return nil }
func (*dummyChain) GetHeaderByNumber(uint64) *types.Header      { return nil }
func (*dummyChain) GetHeaderByHash(common.Hash) *types.Header   { return nil }

// TestGraniteEnforcesZeroBlockGasCost verifies that under the Granite fork
// the consensus path enforces BlockGasCost == 0. This lives in the consensus
// package because the enforcement (rejecting non-zero values) occurs during
// header/block validation (Finalize/FinalizeAndAssemble), while VerifyBlockFee
// only implements the AP4/AP5 fee math and should not be called in Granite.
func TestGraniteEnforcesZeroBlockGasCost(t *testing.T) {
	cfg := paramstest.ForkToChainConfig[upgradetest.Granite]
	chain := &dummyChain{cfg: cfg}
	eng := NewDummyEngine(ConsensusCallbacks{}, NewCoinbaseFaker().consensusMode, nil, nil)
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(nil), nil)
	parent := &types.Header{Time: 0}

	tests := []struct {
		name         string
		blockGasCost *big.Int
		expectedErr  error
	}{
		{
			name:         "zero block gas cost should be ok",
			blockGasCost: big.NewInt(0),
		},
		{
			name:         "non zero block gas cost should error",
			blockGasCost: big.NewInt(1),
			expectedErr:  ErrInvalidBlockGasCost,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := &types.Header{Time: 1, BaseFee: big.NewInt(1)}
			extra := customtypes.GetHeaderExtra(header)
			extra.BlockGasCost = tc.blockGasCost
			extra.ExtDataGasUsed = big.NewInt(0)
			block := customtypes.NewBlockWithExtData(header, nil, nil, nil, nil, nil, false)

			err := eng.Finalize(chain, block, parent, stateDB, nil)
			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
