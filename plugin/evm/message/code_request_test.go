// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
)

// TestMarshalCodeRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeRequest(t *testing.T) {
	codeRequest := CodeRequest{
		Hashes: []common.Hash{common.BytesToHash([]byte("some code pls"))},
	}

	base64CodeRequest := "AAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAHNvbWUgY29kZSBwbHM="

	codeRequestBytes, err := Codec.Marshal(Version, codeRequest)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeRequest, base64.StdEncoding.EncodeToString(codeRequestBytes))

	var c CodeRequest
	_, err = Codec.Unmarshal(codeRequestBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeRequest.Hashes, c.Hashes)
}

// TestMarshalCodeResponse asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeResponse(t *testing.T) {
	codeData := deterministicBytes("code", 50)
	codeResponse := CodeResponse{
		Data: [][]byte{codeData},
	}

	base64CodeResponse := "AAAAAAABAAAAMqWkzsbJB88MjA9jmA3E46NFQ4OcMsZ29w75hOzrBZKYUb1mOf7XwLSCavbAa/5PTM46"
	codeResponseBytes, err := Codec.Marshal(Version, codeResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeResponse, base64.StdEncoding.EncodeToString(codeResponseBytes))

	var c CodeResponse
	_, err = Codec.Unmarshal(codeResponseBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeResponse.Data, c.Data)
}
