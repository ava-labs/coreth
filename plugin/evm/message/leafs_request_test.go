// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

// TestMarshalLeafsRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsRequest(t *testing.T) {
	startBytes := make([]byte, common.HashLength)
	endBytes := make([]byte, common.HashLength)

	for i := range startBytes {
		startBytes[i] = byte(i)
		endBytes[i] = byte(i + 1)
	}

	leafsRequest := LeafsRequest{
		Root:     common.BytesToHash([]byte("im ROOTing for ya")),
		Start:    startBytes,
		End:      endBytes,
		Limit:    1024,
		NodeType: StateTrieNode,
	}

	const base64LeafsRequest = "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8gBAAB"

	leafsRequestBytes, err := Codec.Marshal(Version, leafsRequest)
	assert.NoError(t, err)
	assert.Equal(t, base64LeafsRequest, base64.StdEncoding.EncodeToString(leafsRequestBytes))

	var l LeafsRequest
	_, err = Codec.Unmarshal(leafsRequestBytes, &l)
	assert.NoError(t, err)
	assert.Equal(t, leafsRequest.Root, l.Root)
	assert.Equal(t, leafsRequest.Start, l.Start)
	assert.Equal(t, leafsRequest.End, l.End)
	assert.Equal(t, leafsRequest.Limit, l.Limit)
	assert.Equal(t, leafsRequest.NodeType, l.NodeType)
}

// TestMarshalLeafsResponse asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsResponse(t *testing.T) {
	keysBytes := make([][]byte, 16)
	valsBytes := make([][]byte, 16)
	for i := range keysBytes {
		keysBytes[i] = make([]byte, common.HashLength)
		valsBytes[i] = make([]byte, (i%9)+8) // min 8 bytes, max 16 bytes

		for j := range keysBytes[i] {
			keysBytes[i][j] = byte(j)
		}
		for j := range valsBytes[i] {
			valsBytes[i][j] = byte(j + 1)
		}
	}

	nextKey := make([]byte, common.HashLength)
	for i := range nextKey {
		nextKey[i] = byte(i + 2)
	}

	proofVals := make([][]byte, 4)
	for i := range proofVals {
		proofVals[i] = make([]byte, (i%9)+8) // min 8 bytes, max 16 bytes

		for j := range proofVals[i] {
			proofVals[i][j] = byte(j + 3)
		}
	}

	leafsResponse := LeafsResponse{
		Keys:      keysBytes,
		Vals:      valsBytes,
		More:      true,
		ProofVals: proofVals,
	}

	const base64LeafsResponse = "AAAAAAAQAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAAEAAAAAgBAgMEBQYHCAAAAAkBAgMEBQYHCAkAAAAKAQIDBAUGBwgJCgAAAAsBAgMEBQYHCAkKCwAAAAwBAgMEBQYHCAkKCwwAAAANAQIDBAUGBwgJCgsMDQAAAA4BAgMEBQYHCAkKCwwNDgAAAA8BAgMEBQYHCAkKCwwNDg8AAAAQAQIDBAUGBwgJCgsMDQ4PEAAAAAgBAgMEBQYHCAAAAAkBAgMEBQYHCAkAAAAKAQIDBAUGBwgJCgAAAAsBAgMEBQYHCAkKCwAAAAwBAgMEBQYHCAkKCwwAAAANAQIDBAUGBwgJCgsMDQAAAA4BAgMEBQYHCAkKCwwNDgAAAAQAAAAIAwQFBgcICQoAAAAJAwQFBgcICQoLAAAACgMEBQYHCAkKCwwAAAALAwQFBgcICQoLDA0="

	leafsResponseBytes, err := Codec.Marshal(Version, leafsResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64LeafsResponse, base64.StdEncoding.EncodeToString(leafsResponseBytes))

	var l LeafsResponse
	_, err = Codec.Unmarshal(leafsResponseBytes, &l)
	assert.NoError(t, err)
	assert.Equal(t, leafsResponse.Keys, l.Keys)
	assert.Equal(t, leafsResponse.Vals, l.Vals)
	assert.False(t, l.More) // make sure it is not serialized
	assert.Equal(t, leafsResponse.ProofVals, l.ProofVals)
}

func TestLeafsRequestValidation(t *testing.T) {
	mockRequestHandler := &mockHandler{}

	tests := map[string]struct {
		request        LeafsRequest
		assertResponse func(t *testing.T)
	}{
		"node type StateTrieNode": {
			request: LeafsRequest{
				Root:     common.BytesToHash([]byte("some hash goes here")),
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    10,
				NodeType: StateTrieNode,
			},
			assertResponse: func(t *testing.T) {
				assert.True(t, mockRequestHandler.handleStateTrieCalled)
				assert.False(t, mockRequestHandler.handleAtomicTrieCalled)
				assert.False(t, mockRequestHandler.handleBlockRequestCalled)
				assert.False(t, mockRequestHandler.handleCodeRequestCalled)
			},
		},
		"node type AtomicTrieNode": {
			request: LeafsRequest{
				Root:     common.BytesToHash([]byte("some hash goes here")),
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    10,
				NodeType: AtomicTrieNode,
			},
			assertResponse: func(t *testing.T) {
				assert.False(t, mockRequestHandler.handleStateTrieCalled)
				assert.True(t, mockRequestHandler.handleAtomicTrieCalled)
				assert.False(t, mockRequestHandler.handleBlockRequestCalled)
				assert.False(t, mockRequestHandler.handleCodeRequestCalled)
			},
		},
		"unknown node type": {
			request: LeafsRequest{
				Root:     common.BytesToHash([]byte("some hash goes here")),
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    10,
				NodeType: NodeType(11),
			},
			assertResponse: func(t *testing.T) {
				assert.False(t, mockRequestHandler.handleStateTrieCalled)
				assert.False(t, mockRequestHandler.handleAtomicTrieCalled)
				assert.False(t, mockRequestHandler.handleBlockRequestCalled)
				assert.False(t, mockRequestHandler.handleCodeRequestCalled)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, _ = test.request.Handle(context.Background(), ids.GenerateTestNodeID(), 1, mockRequestHandler)
			test.assertResponse(t)
			mockRequestHandler.reset()
		})
	}
}

var _ RequestHandler = &mockHandler{}

type mockHandler struct {
	handleStateTrieCalled,
	handleAtomicTrieCalled,
	handleBlockRequestCalled,
	handleCodeRequestCalled,
	handleMessageSignatureCalled,
	handleBlockSignatureCalled bool
}

func (m *mockHandler) HandleStateTrieLeafsRequest(context.Context, ids.NodeID, uint32, LeafsRequest) ([]byte, error) {
	m.handleStateTrieCalled = true
	return nil, nil
}

func (m *mockHandler) HandleAtomicTrieLeafsRequest(context.Context, ids.NodeID, uint32, LeafsRequest) ([]byte, error) {
	m.handleAtomicTrieCalled = true
	return nil, nil
}

func (m *mockHandler) HandleBlockRequest(context.Context, ids.NodeID, uint32, BlockRequest) ([]byte, error) {
	m.handleBlockRequestCalled = true
	return nil, nil
}

func (m *mockHandler) HandleCodeRequest(context.Context, ids.NodeID, uint32, CodeRequest) ([]byte, error) {
	m.handleCodeRequestCalled = true
	return nil, nil
}

func (m *mockHandler) HandleMessageSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest MessageSignatureRequest) ([]byte, error) {
	m.handleMessageSignatureCalled = true
	return nil, nil
}
func (m *mockHandler) HandleBlockSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest BlockSignatureRequest) ([]byte, error) {
	m.handleBlockSignatureCalled = true
	return nil, nil
}

func (m *mockHandler) reset() {
	m.handleStateTrieCalled = false
	m.handleAtomicTrieCalled = false
	m.handleBlockRequestCalled = false
	m.handleCodeRequestCalled = false
}
