// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/ava-labs/coreth/sync/handlers/stats"

	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/triedb"
)

// atomicLeafHandler is a wrapper around handlers.LeafRequestHandler that allows for initialization after creation
type atomicLeafHandler struct {
	handlers.LeafRequestHandler
}

// Initialize initializes the atomicLeafHandler with the provided atomicTrieDB, trieKeyLength, and networkCodec
func NewAtomicLeafHandler(atomicTrieDB *triedb.Database, trieKeyLength int, networkCodec codec.Manager) *atomicLeafHandler {
	handlerStats := stats.GetOrRegisterHandlerStats(metrics.Enabled)
	return &atomicLeafHandler{
		LeafRequestHandler: handlers.NewLeafsRequestHandler(atomicTrieDB, trieKeyLength, nil, networkCodec, handlerStats),
	}
}
