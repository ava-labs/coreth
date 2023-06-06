// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/mempool"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/txgossip"
)

var _ txgossip.Gossiper[*MempoolTx] = (*atomicTxGossiper)(nil)

type atomicTxGossiper struct {
	mempool *Mempool
}

func (a atomicTxGossiper) Mempool() mempool.Mempool[*MempoolTx] {
	return a.mempool
}

func (a atomicTxGossiper) GossipRequest() message.MempoolTxsRequest {
	return &message.MempoolAtomicTxsRequest{}
}

func (a atomicTxGossiper) Tx() *MempoolTx {
	return &MempoolTx{}
}

var _ txgossip.Gossiper[*core.MempoolTx] = (*ethTxGossiper)(nil)

type ethTxGossiper struct {
	txPool *core.TxPool
}

func (e *ethTxGossiper) Mempool() mempool.Mempool[*core.MempoolTx] {
	return e.txPool
}

func (e *ethTxGossiper) GossipRequest() message.MempoolTxsRequest {
	return &message.MempoolEthTxsRequest{}
}

func (e *ethTxGossiper) Tx() *core.MempoolTx {
	return &core.MempoolTx{}
}
