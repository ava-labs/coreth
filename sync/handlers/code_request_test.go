// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
)

func TestCodeRequestHandler(t *testing.T) {
	database := memorydb.New()

	codeBytes := []byte("some code goes here")
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	mockHandlerStats := &stats.MockHandlerStats{}
	codeRequestHandler := NewCodeRequestHandler(database, message.Codec, mockHandlerStats)

	// query for known code entry
	hashes := []common.Hash{codeHash}
	responseBytes, err := codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestNodeID(), 1, message.CodeRequest{Hashes: hashes})
	assert.NoError(t, err)

	var response message.CodeResponse
	if _, err = message.Codec.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("error unmarshalling CodeResponse", err)
	}
	assert.Len(t, response.Data, len(hashes))
	assert.True(t, bytes.Equal(codeBytes, response.Data[0]))
	assert.EqualValues(t, 1, mockHandlerStats.CodeRequestCount)
	assert.EqualValues(t, len(response.Data[0]), mockHandlerStats.CodeBytesReturnedSum)
	mockHandlerStats.Reset()

	// query for non-unique hashes
	hashes = make([]common.Hash, maxCodeHashesPerRequest)
	for i := 0; i < len(hashes); i++ {
		hashes[i] = codeHash
	}
	responseBytes, err = codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestNodeID(), 2, message.CodeRequest{Hashes: hashes})
	assert.NoError(t, err)
	assert.Nil(t, responseBytes)
	assert.EqualValues(t, 1, mockHandlerStats.DuplicateHashesRequested)
	mockHandlerStats.Reset()

	// query for too many hashes
	hashes = append(hashes, codeHash)
	responseBytes, err = codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestNodeID(), 3, message.CodeRequest{Hashes: hashes})
	assert.NoError(t, err)
	assert.Nil(t, responseBytes)
	assert.EqualValues(t, 1, mockHandlerStats.TooManyHashesRequested)
	mockHandlerStats.Reset()

	// query for missing code entry
	hashes = []common.Hash{common.BytesToHash([]byte("some unknown hash"))}
	responseBytes, err = codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestNodeID(), 4, message.CodeRequest{Hashes: hashes})
	assert.NoError(t, err)
	assert.Nil(t, responseBytes)
	assert.EqualValues(t, 1, mockHandlerStats.MissingCodeHashCount)
	mockHandlerStats.Reset()

	// assert max size code bytes are handled
	codeBytes = make([]byte, params.MaxCodeSize)
	n, err := rand.Read(codeBytes)
	assert.NoError(t, err)
	assert.Equal(t, params.MaxCodeSize, n)
	codeHash = crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	hashes = []common.Hash{codeHash}
	responseBytes, err = codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestNodeID(), 5, message.CodeRequest{Hashes: hashes})
	assert.NoError(t, err)
	assert.NotNil(t, responseBytes)

	response = message.CodeResponse{}
	if _, err = message.Codec.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("error unmarshalling CodeResponse", err)
	}
	assert.Len(t, response.Data, 1)
	assert.True(t, bytes.Equal(codeBytes, response.Data[0]))
	assert.EqualValues(t, 1, mockHandlerStats.CodeRequestCount)
	assert.EqualValues(t, len(response.Data[0]), mockHandlerStats.CodeBytesReturnedSum)
}
