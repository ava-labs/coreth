// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/plugin/evm/message"
)

type MempoolTxsRequestHandler[T gossip.Tx] struct {
	mempool gossip.PendingTxs[T]
	codec   codec.Manager
}

func (m *MempoolTxsRequestHandler[T]) OnMempoolTxRequest(_ context.Context, _ ids.NodeID, _ uint32, request message.MempoolTxsRequest) ([]byte, error) {
	peerFilter, err := gossip.NewBloomFilterFromBytes(request.GetBloomFilter())
	if err != nil {
		return nil, err
	}
	result := make([][]byte, 0)
	for _, tx := range m.mempool.GetPendingTxs() {
		if peerFilter.Contains(tx.Hash()) {
			continue
		}

		bytes, err := tx.Marshal()
		if err != nil {
			return nil, err
		}
		result = append(result, bytes)
	}

	response := message.MempoolTxsResponse{
		Txs: result,
	}

	bytes, err := m.codec.Marshal(message.Version, response)
	return bytes, err
}
