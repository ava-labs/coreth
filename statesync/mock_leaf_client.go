// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers"
	"github.com/ethereum/go-ethereum/common"
)

var _ Client = &mockClient{}

type mockClient struct {
	codec         codec.Manager
	leafsHandler  *handlers.LeafsRequestHandler
	codesHandler  *handlers.CodeRequestHandler
	blocksHandler *handlers.BlockRequestHandler
	// GetLeafsIntercept is called on every GetLeafs request if set to a non-nil callback.
	// Takes in the result returned by the handler and can return a replacement response or
	// error.
	GetLeafsIntercept func(message.LeafsResponse) (message.LeafsResponse, error)
	// GetCodesIntercept is called on every GetCode request if set to a non-nil callback.
	// Takes in the result returned by the handler and can return a replacement response or
	// error.
	GetCodeIntercept func(message.CodeResponse) ([]byte, error)
}

func NewMockLeafClient(
	codec codec.Manager,
	leafHandler *handlers.LeafsRequestHandler,
	codesHandler *handlers.CodeRequestHandler,
	blocksHandler *handlers.BlockRequestHandler,
) *mockClient {
	return &mockClient{
		codec:         codec,
		leafsHandler:  leafHandler,
		codesHandler:  codesHandler,
		blocksHandler: blocksHandler,
	}
}

func (ml *mockClient) GetLeafs(request message.LeafsRequest) (message.LeafsResponse, error) {
	response, err := ml.leafsHandler.OnLeafsRequest(context.Background(), ids.GenerateTestShortID(), 1, request)
	if err != nil {
		return message.LeafsResponse{}, err
	}

	leafResponseIntf, err := parseLeafsResponse(ml.codec, request, response)
	if err != nil {
		return message.LeafsResponse{}, err
	}
	leafsResponse := leafResponseIntf.(message.LeafsResponse)
	if ml.GetLeafsIntercept != nil {
		return ml.GetLeafsIntercept(leafsResponse)
	}
	return leafsResponse, nil
}

func (ml *mockClient) GetCode(codeHash common.Hash) ([]byte, error) {
	if ml.codesHandler == nil {
		panic("no code handler for mock client")
	}
	request := message.CodeRequest{Hash: codeHash}
	response, err := ml.codesHandler.OnCodeRequest(context.Background(), ids.GenerateTestShortID(), 1, request)
	if err != nil {
		return nil, err
	}

	codeResponseIntf, err := parseCode(ml.codec, request, response)
	if err != nil {
		return nil, err
	}
	codeResponse := codeResponseIntf.(message.CodeResponse)
	if ml.GetCodeIntercept != nil {
		return ml.GetCodeIntercept(codeResponse)
	}
	return codeResponse.Data, nil
}

func (ml *mockClient) GetBlocks(blockHash common.Hash, height uint64, numParents uint16) ([]*types.Block, error) {
	if ml.blocksHandler == nil {
		panic("no blocks handler for mock client")
	}
	request := message.BlockRequest{
		Hash:    blockHash,
		Height:  height,
		Parents: numParents,
	}
	response, err := ml.blocksHandler.OnBlockRequest(context.Background(), ids.GenerateTestShortID(), 1, request)
	if err != nil {
		return nil, err
	}

	blocks, err := parseBlocks(ml.codec, request, response)
	if err != nil {
		return nil, err
	}
	return blocks.(types.Blocks), nil
}
