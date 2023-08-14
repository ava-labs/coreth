// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGossiperShutdown(t *testing.T) {
	config := Config{Frequency: time.Second}
	gossiper := NewGossiper[testTx](config, nil, nil, nil, 0)
	done := make(chan struct{})
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go gossiper.Gossip(done, wg)

	close(done)
	wg.Wait()
}

func TestGossiperGossip(t *testing.T) {
	tests := []struct {
		name      string
		requester []*testTx // what we have
		responder []*testTx // what the peer we're requesting gossip from has
		expected  []*testTx // what we should have after a gossip cycle
	}{
		{
			name: "no gossip - no one knows anything",
		},
		{
			name:      "no gossip - requester knows more than responder",
			requester: []*testTx{{hash: Hash{0}}},
			expected:  []*testTx{{hash: Hash{0}}},
		},
		{
			name:      "no gossip - requester knows everything responder knows",
			requester: []*testTx{{hash: Hash{0}}},
			responder: []*testTx{{hash: Hash{0}}},
			expected:  []*testTx{{hash: Hash{0}}},
		},
		{
			name:      "gossip - requester knows nothing",
			responder: []*testTx{{hash: Hash{0}}},
			expected:  []*testTx{{hash: Hash{0}}},
		},
		{
			name:      "gossip - requester knows less than responder",
			requester: []*testTx{{hash: Hash{0}}},
			responder: []*testTx{{hash: Hash{0}}, {hash: Hash{1}}},
			expected:  []*testTx{{hash: Hash{0}}, {hash: Hash{1}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			cc := codec.NewManager(units.MiB)
			lc := linearcodec.NewDefault()
			require.NoError(lc.RegisterType(PullGossipRequest{}))
			require.NoError(lc.RegisterType(PullGossipResponse{}))
			require.NoError(cc.RegisterCodec(0, lc))

			responseSender := common.NewMockSender(ctrl)
			responseRouter := p2p.NewRouter(logging.NoLog{}, responseSender)
			responseBloom, err := NewBloomFilter(1000, 0.01)
			require.NoError(err)
			responseSet := testSet{
				set:   set.Set[*testTx]{},
				bloom: responseBloom,
			}
			for _, item := range tt.responder {
				require.NoError(responseSet.Add(item))
			}
			handler := NewHandler[*testTx](responseSet, cc, 0)
			_, err = responseRouter.RegisterAppProtocol(0x0, handler)
			require.NoError(err)

			requestSender := common.NewMockSender(ctrl)
			requestRouter := p2p.NewRouter(logging.NoLog{}, requestSender)
			require.NoError(requestRouter.Connected(context.Background(), ids.EmptyNodeID, nil))

			gossiped := make(chan struct{})
			requestSender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) {
					go func() {
						require.NoError(responseRouter.AppRequest(ctx, ids.EmptyNodeID, requestID, time.Time{}, request))
					}()
				}).AnyTimes()

			responseSender.EXPECT().
				SendAppResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) {
					require.NoError(requestRouter.AppResponse(ctx, nodeID, requestID, appResponseBytes))
					close(gossiped)
				}).AnyTimes()

			bloom, err := NewBloomFilter(1000, 0.01)
			require.NoError(err)
			requestSet := testSet{
				set:   set.Set[*testTx]{},
				bloom: bloom,
			}
			for _, item := range tt.requester {
				require.NoError(requestSet.Add(item))
			}

			requestClient, err := requestRouter.RegisterAppProtocol(0x0, nil)
			require.NoError(err)

			config := Config{
				Frequency: 500 * time.Millisecond,
				PollSize:  1,
			}
			gossiper := NewGossiper[testTx, *testTx](config, requestSet, requestClient, cc, 0)
			done := make(chan struct{})
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go gossiper.Gossip(done, wg)
			<-gossiped

			require.Len(requestSet.set, len(tt.expected))
			for _, expected := range tt.expected {
				require.Contains(requestSet.set, expected)
			}

			close(done)
			wg.Wait()
		})
	}
}
