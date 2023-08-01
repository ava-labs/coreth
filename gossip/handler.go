// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

func NewHandler[T Gossipable](set Set[T], codec codec.Manager, codecVersion uint16) *Handler[T] {
	return &Handler[T]{
		set:          set,
		codec:        codec,
		codecVersion: codecVersion,
	}
}

type Handler[T Gossipable] struct {
	set          Set[T]
	codec        codec.Manager
	codecVersion uint16
}

func (h Handler[T]) AppGossip(context.Context, ids.NodeID, []byte) error {
	return nil
}

func (h Handler[T]) AppRequest(_ context.Context, _ ids.NodeID, _ uint32, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := PullGossipRequest{}
	if _, err := h.codec.Unmarshal(requestBytes, &request); err != nil {
		return nil, err
	}
	peerFilter := &bloomfilter.Filter{}
	if err := peerFilter.UnmarshalBinary(request.BloomFilter); err != nil {
		return nil, err
	}

	// filter out what the requesting peer already knows about
	unknown := h.set.Get(func(gossipable T) bool {
		return !peerFilter.Contains(NewHasher(gossipable.GetID()))
	})
	gossipBytes := make([][]byte, 0, len(unknown))
	for _, gossipable := range unknown {
		bytes, err := gossipable.Marshal()
		if err != nil {
			return nil, err
		}
		gossipBytes = append(gossipBytes, bytes)
	}

	response := PullGossipResponse{
		GossipBytes: gossipBytes,
	}
	responseBytes, err := h.codec.Marshal(h.codecVersion, response)
	if err != nil {
		return nil, err
	}

	return responseBytes, nil
}

func (Handler[T]) CrossChainAppRequest(context.Context, ids.ID, uint32, time.Time, []byte) ([]byte, error) {
	return nil, nil
}
