// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ MempoolTxsRequest = (*MempoolEthTxsRequest)(nil)
var _ MempoolTxsRequest = (*MempoolAtomicTxsRequest)(nil)

// MempoolTxsRequest is a request to initiate a tx gossip session between two
// nodes.
type MempoolTxsRequest interface {
	Request
	GetBloomFilter() []byte
	SetBloomFilter([]byte)
}

type MempoolEthTxsRequest struct {
	// BloomFilter describes the contents of the requesting node's mempool so
	// the responding peer can gossip transactions that the requesting node has
	// not learned about yet.
	BloomFilter []byte `serialize:"true"`
}

func (m *MempoolEthTxsRequest) GetBloomFilter() []byte {
	return m.BloomFilter
}

func (m *MempoolEthTxsRequest) SetBloomFilter(bytes []byte) {
	m.BloomFilter = bytes
}

func (m *MempoolEthTxsRequest) String() string {
	return fmt.Sprintf("MempoolEthTxsRequest(BloomFilter=%s)", m.BloomFilter)
}

func (m *MempoolEthTxsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleMempoolEthTxsRequest(ctx, nodeID, requestID, m)
}

type MempoolAtomicTxsRequest struct {
	// BloomFilter describes the contents of the requesting node's mempool so
	// the responding peer can gossip transactions that the requesting node has
	// not learned about yet.
	BloomFilter []byte `serialize:"true"`
}

func (m *MempoolAtomicTxsRequest) GetBloomFilter() []byte {
	return m.BloomFilter
}

func (m *MempoolAtomicTxsRequest) SetBloomFilter(bytes []byte) {
	m.BloomFilter = bytes
}

func (m *MempoolAtomicTxsRequest) String() string {
	return fmt.Sprintf("MempoolAtomicTxsRequest(BloomFilter=%s)", m.BloomFilter)
}

func (m *MempoolAtomicTxsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleMempoolAtomicTxsRequest(ctx, nodeID, requestID, m)
}

type MempoolTxsResponse struct {
	Txs [][]byte `serialize:"true"`
}
