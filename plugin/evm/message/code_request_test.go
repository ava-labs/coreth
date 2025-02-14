// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
	codeData := make([]byte, 50)
	for i := range codeData {
		codeData[i] = byte(i)
	}

	codeResponse := CodeResponse{
		Data: [][]byte{codeData},
	}

	const base64CodeResponse = "AAAAAAABAAAAMgABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fICEiIyQlJicoKSorLC0uLzAx"

	codeResponseBytes, err := Codec.Marshal(Version, codeResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeResponse, base64.StdEncoding.EncodeToString(codeResponseBytes))

	var c CodeResponse
	_, err = Codec.Unmarshal(codeResponseBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeResponse.Data, c.Data)
}
