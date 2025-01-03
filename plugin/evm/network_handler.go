// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncHandlers "github.com/ava-labs/coreth/sync/handlers"
	syncStats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/warp"
	warpHandlers "github.com/ava-labs/coreth/warp/handlers"
	"github.com/ethereum/go-ethereum/ethdb"
)

var _ message.RequestHandler = &networkHandler{}

type networkHandler struct {
	leafRequestHandlers     map[message.NodeType]*syncHandlers.LeafsRequestHandler
	blockRequestHandler     *syncHandlers.BlockRequestHandler
	codeRequestHandler      *syncHandlers.CodeRequestHandler
	signatureRequestHandler *warpHandlers.SignatureRequestHandler
}

type LeafRequestTypeConfig struct {
	NodeType     message.NodeType
	NodeKeyLen   int
	TrieDB       *triedb.Database
	UseSnapshots bool
	MetricName   string
}

// newNetworkHandler constructs the handler for serving network requests.
func newNetworkHandler(
	provider syncHandlers.SyncDataProvider,
	diskDB ethdb.KeyValueReader,
	warpBackend warp.Backend,
	networkCodec codec.Manager,
	leafRequesTypeConfigs map[message.NodeType]LeafRequestTypeConfig,
) message.RequestHandler {
	syncStats := syncStats.NewHandlerStats(metrics.Enabled)
	leafRequestHandlers := make(map[message.NodeType]*syncHandlers.LeafsRequestHandler)
	for _, config := range leafRequesTypeConfigs {
		snapshotProvider := provider
		if !config.UseSnapshots {
			snapshotProvider = nil
		}
		leafRequestHandler := syncHandlers.NewLeafsRequestHandler(config.TrieDB, config.NodeKeyLen, snapshotProvider, networkCodec, syncStats)
		leafRequestHandlers[config.NodeType] = leafRequestHandler
	}
	return &networkHandler{
		leafRequestHandlers:     leafRequestHandlers,
		blockRequestHandler:     syncHandlers.NewBlockRequestHandler(provider, networkCodec, syncStats),
		codeRequestHandler:      syncHandlers.NewCodeRequestHandler(diskDB, networkCodec, syncStats),
		signatureRequestHandler: warpHandlers.NewSignatureRequestHandler(warpBackend, networkCodec),
	}
}

func (n networkHandler) HandleLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	handler, ok := n.leafRequestHandlers[leafsRequest.NodeType]
	if !ok {
		return nil, fmt.Errorf("unknown node type %d", leafsRequest.NodeType)
	}
	return handler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (n networkHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return n.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (n networkHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return n.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

func (n networkHandler) HandleMessageSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, messageSignatureRequest message.MessageSignatureRequest) ([]byte, error) {
	return n.signatureRequestHandler.OnMessageSignatureRequest(ctx, nodeID, requestID, messageSignatureRequest)
}

func (n networkHandler) HandleBlockSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockSignatureRequest message.BlockSignatureRequest) ([]byte, error) {
	return n.signatureRequestHandler.OnBlockSignatureRequest(ctx, nodeID, requestID, blockSignatureRequest)
}
