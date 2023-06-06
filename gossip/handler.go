// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/sdk/p2p"
)

func NewHandler[T any, U TxConstraint[T]](mempool Mempool[U], codec codec.Manager, codecVersion uint16) p2p.Handler {
	return &Handler[T, U]{
		mempool:      mempool,
		codec:        codec,
		codecVersion: codecVersion,
	}
}

type Handler[T any, U TxConstraint[T]] struct {
	mempool      Mempool[U]
	codec        codec.Manager
	codecVersion uint16
}

func (h Handler[T, U]) AppGossip(context.Context, ids.NodeID, []byte) error {
	return nil
}

func (h Handler[T, U]) AppRequest(_ context.Context, nodeID ids.NodeID, _ uint32, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := PullTxsRequest{}
	if _, err := h.codec.Unmarshal(requestBytes, &request); err != nil {
		return nil, err
	}
	peerFilter := &bloomfilter.Filter{}
	if err := peerFilter.UnmarshalBinary(request.BloomFilter); err != nil {
		return nil, err
	}

	unknownTxs := h.mempool.GetTxs(func(tx U) bool {
		return !peerFilter.Contains(NewHasher(tx.ID()))
	})
	txs := make([][]byte, 0, len(unknownTxs))
	for _, tx := range unknownTxs {
		bytes, err := tx.Marshal()
		if err != nil {
			return nil, err
		}
		txs = append(txs, bytes)
	}

	response := PullTxsResponse{
		Txs: txs,
	}
	responseBytes, err := h.codec.Marshal(h.codecVersion, response)
	if err != nil {
		return nil, err
	}

	return responseBytes, nil
}

func (Handler[T, U]) CrossChainAppRequest(context.Context, ids.ID, uint32, time.Time, []byte) ([]byte, error) {
	return nil, nil
}
