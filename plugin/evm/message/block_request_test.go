// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
)

// TestMarshalBlockRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalBlockRequest(t *testing.T) {
	blockRequest := BlockRequest{
		Hash:    common.BytesToHash([]byte("some hash is here yo")),
		Height:  1337,
		Parents: 64,
	}

	base64BlockRequest := "AAAAAAAAAAAAAAAAAABzb21lIGhhc2ggaXMgaGVyZSB5bwAAAAAAAAU5AEA="

	blockRequestBytes, err := Codec.Marshal(Version, blockRequest)
	assert.NoError(t, err)
	assert.Equal(t, base64BlockRequest, base64.StdEncoding.EncodeToString(blockRequestBytes))

	var b BlockRequest
	_, err = Codec.Unmarshal(blockRequestBytes, &b)
	assert.NoError(t, err)
	assert.Equal(t, blockRequest.Hash, b.Hash)
	assert.Equal(t, blockRequest.Height, b.Height)
	assert.Equal(t, blockRequest.Parents, b.Parents)
}

// TestMarshalBlockResponse asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalBlockResponse(t *testing.T) {
	blocksBytes := make([][]byte, 32)
	for i := range blocksBytes {
		size := 32 + (i % 33)
		blocksBytes[i] = deterministicBytes(fmt.Sprintf("block-%d", i), size)
	}

	blockResponse := BlockResponse{
		Blocks: blocksBytes,
	}

	base64BlockResponse := "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"

	blockResponseBytes, err := Codec.Marshal(Version, blockResponse)
	assert.NoError(t, err)
	encoded := base64.StdEncoding.EncodeToString(blockResponseBytes)
	t.Log("BlockResponse base64:", encoded)
	assert.Equal(t, base64BlockResponse, encoded)

	var b BlockResponse
	_, err = Codec.Unmarshal(blockResponseBytes, &b)
	assert.NoError(t, err)
	assert.Equal(t, blockResponse.Blocks, b.Blocks)
}
