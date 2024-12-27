// (c) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestBlockChainAPI_isAllowedToBeQueried(t *testing.T) {
	t.Parallel()

	const recentBlocksWindow uint64 = 1024

	makeBlockWithNumber := func(number uint64) *types.Block {
		header := &types.Header{
			Number: big.NewInt(int64(number)),
		}
		return types.NewBlock(header, nil, nil, nil, nil)
	}

	testCases := map[string]struct {
		blockNumOrHash rpc.BlockNumberOrHash
		makeBackend    func(ctrl *gomock.Controller) *MockBackend
		wantErrMessage string
	}{
		"historical_proofs_accepted": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(0)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().HistoricalConfig().Return(true, recentBlocksWindow)
				return backend
			},
		},
		"block_number_recent_below_window": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().HistoricalConfig().Return(false, recentBlocksWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(1020))
				return backend
			},
		},
		"block_number_recent": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(2000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().HistoricalConfig().Return(false, recentBlocksWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				return backend
			},
		},
		"block_number_recent_by_hash": {
			blockNumOrHash: rpc.BlockNumberOrHashWithHash(common.Hash{99}, false),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().HistoricalConfig().Return(false, recentBlocksWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				backend.EXPECT().
					BlockByNumberOrHash(gomock.Any(), gomock.Any()).
					Return(makeBlockWithNumber(2000), nil)
				return backend
			},
		},
		"block_number_recent_by_hash_error": {
			blockNumOrHash: rpc.BlockNumberOrHashWithHash(common.Hash{99}, false),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().HistoricalConfig().Return(false, recentBlocksWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				backend.EXPECT().
					BlockByNumberOrHash(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("test error"))
				return backend
			},
			wantErrMessage: "getting block from hash: test error",
		},
		"block_number_historical": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().HistoricalConfig().Return(false, recentBlocksWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				return backend
			},
			wantErrMessage: "block number 1000 is before the oldest non-historical block number 1176 (window of 1024 blocks)",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)

			api := &BlockChainAPI{
				b: testCase.makeBackend(ctrl),
			}

			err := api.isAllowedToBeQueried(testCase.blockNumOrHash)
			if testCase.wantErrMessage == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, testCase.wantErrMessage)
			}
		})
	}
}
