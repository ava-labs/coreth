package vm

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/triedb"
)

var errUninitialized = errors.New("uninitialized handler")

type uninitializedHandler struct{}

func (h *uninitializedHandler) OnLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return nil, errUninitialized
}

type atomicLeafHandler struct {
	handlers.LeafRequestHandler
}

func NewAtomicLeafHandler() *atomicLeafHandler {
	return &atomicLeafHandler{
		LeafRequestHandler: &uninitializedHandler{},
	}
}

func (a *atomicLeafHandler) Initialize(atomicTrieDB *triedb.Database, trieKeyLength int, networkCodec codec.Manager) {
	handlerStats := stats.NewHandlerStats(metrics.Enabled)
	a.LeafRequestHandler = handlers.NewLeafsRequestHandler(atomicTrieDB, trieKeyLength, nil, networkCodec, handlerStats)
}
