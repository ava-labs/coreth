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
	require := require.New(t)
	ctrl := gomock.NewController(t)

	cc := codec.NewManager(2 * units.MiB)
	lc := linearcodec.NewDefault()
	require.NoError(lc.RegisterType(PullGossipRequest{}))
	require.NoError(lc.RegisterType(PullGossipResponse{}))
	require.NoError(cc.RegisterCodec(0, lc))

	responseSender := common.NewMockSender(ctrl)
	responseRouter := p2p.NewRouter(logging.NoLog{}, responseSender)
	responseSet := testSet{
		set: set.Set[*testTx]{},
	}
	tx := &testTx{hash: Hash{1, 2, 3, 4, 5}}
	require.NoError(responseSet.Add(tx))
	handler := NewHandler[*testTx](responseSet, cc, 0)
	_, err := responseRouter.RegisterAppProtocol(0x0, handler)
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
			go func() {
				require.NoError(requestRouter.AppResponse(ctx, nodeID, requestID, appResponseBytes))
				close(gossiped)
			}()
		}).AnyTimes()

	bloom, err := NewBloomFilter(1000, 0.01)
	require.NoError(err)
	requestSet := testSet{
		set:   set.Set[*testTx]{},
		bloom: bloom,
	}
	requestClient, err := requestRouter.RegisterAppProtocol(0x0, nil)
	require.NoError(err)

	// We shouldn't have gotten any gossip before the test started
	require.Len(requestSet.set, 0)

	config := Config{
		Frequency: time.Second,
		PollSize:  1,
	}
	gossiper := NewGossiper[testTx, *testTx](config, requestSet, requestClient, cc, 0)
	done := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go gossiper.Gossip(done, wg)
	<-gossiped

	require.Len(requestSet.set, 1)
	require.Contains(requestSet.set, tx)

	close(done)
	wg.Wait()
}
