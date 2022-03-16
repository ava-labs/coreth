// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
)

const (
	defaultServerCacheSize int = 75
)

var _ message.RequestHandler = &syncHandler{}

type syncHandler struct {
	stateTrieLeafsRequestHandler  *LeafsRequestHandler
	atomicTrieLeafsRequestHandler *LeafsRequestHandler
	blockRequestHandler           *BlockRequestHandler
	codeRequestHandler            *CodeRequestHandler
}

// NewSyncHandler constructs the handler for serving fast sync.
func NewSyncHandler(
	blockchain *core.BlockChain,
	atomicTrieDB *trie.Database,
	networkCodec codec.Manager,
	stats stats.HandlerStats,
) message.RequestHandler {
	// Create separate EVM TrieDB (read only) for serving leafs requests.
	// We create a separate TrieDB here, so that it has a separate cache from the one
	// used by the node when processing blocks.
	evmTrieDB := trie.NewDatabaseWithConfig(
		blockchain.StateCache().TrieDB().DiskDB(),
		&trie.Config{
			Cache: defaultServerCacheSize,
		},
	)
	return &syncHandler{
		stateTrieLeafsRequestHandler:  NewLeafsRequestHandler(evmTrieDB, networkCodec, stats), // TODO standardize argument ordering
		atomicTrieLeafsRequestHandler: NewLeafsRequestHandler(atomicTrieDB, networkCodec, stats),
		blockRequestHandler:           NewBlockRequestHandler(blockchain.GetBlock, networkCodec, stats),
		codeRequestHandler:            NewCodeRequestHandler(evmTrieDB.DiskDB(), networkCodec, stats),
	}
}

func (s *syncHandler) HandleStateTrieLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return s.stateTrieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (s *syncHandler) HandleAtomicTrieLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return s.atomicTrieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (s *syncHandler) HandleBlockRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return s.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (s *syncHandler) HandleCodeRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return s.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}
