// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/golang/protobuf/proto"
	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	"github.com/ava-labs/coreth/gossip/proto/pb"
)

var _ p2p.Handler = (*Handler[Gossipable])(nil)

func NewHandler[T Gossipable](set Set[T]) *Handler[T] {
	return &Handler[T]{
		Handler: p2p.NoOpHandler{},
		set:     set,
	}
}

type Handler[T Gossipable] struct {
	p2p.Handler
	set Set[T]
}

func (h Handler[T]) AppRequest(_ context.Context, nodeID ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := &pb.PullGossipRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		log.Debug("failed to unmarshal gossip request", "nodeID", nodeID, "err", err)
		return nil, nil
	}

	filter := &BloomFilter{
		Bloom: &bloomfilter.Filter{},
		Salt:  request.Salt,
	}
	if err := filter.Bloom.UnmarshalBinary(request.Filter); err != nil {
		log.Debug("failed to unmarshal bloom filter", "nodeID", nodeID, "err", err)
		return nil, nil
	}

	// filter out what the requesting peer already knows about
	unknown := h.set.Get(func(gossipable T) bool {
		return !filter.Has(gossipable)
	})

	gossipBytes := make([][]byte, 0, len(unknown))
	for _, gossipable := range unknown {
		bytes, err := gossipable.Marshal()
		if err != nil {
			return nil, err
		}
		gossipBytes = append(gossipBytes, bytes)
	}

	response := &pb.PullGossipResponse{
		Gossip: gossipBytes,
	}

	return proto.Marshal(response)
}
