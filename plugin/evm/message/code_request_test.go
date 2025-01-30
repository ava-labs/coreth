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
	codeData := []byte{0x52, 0xfd, 0xfc, 0x7, 0x21, 0x82, 0x65, 0x4f, 0x16, 0x3f, 0x5f, 0xf, 0x9a, 0x62, 0x1d, 0x72, 0x95, 0x66, 0xc7, 0x4d, 0x10, 0x3, 0x7c, 0x4d, 0x7b, 0xbb, 0x4, 0x7, 0xd1, 0xe2, 0xc6, 0x49, 0x81, 0x85, 0x5a, 0xd8, 0x68, 0x1d, 0xd, 0x86, 0xd1, 0xe9, 0x1e, 0x0, 0x16, 0x79, 0x39, 0xcb, 0x66, 0x94}

	codeResponse := CodeResponse{
		Data: [][]byte{codeData},
	}

	base64CodeResponse := "AAAAAAABAAAAMlL9/AchgmVPFj9fD5piHXKVZsdNEAN8TXu7BAfR4sZJgYVa2GgdDYbR6R4AFnk5y2aU"

	codeResponseBytes, err := Codec.Marshal(Version, codeResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeResponse, base64.StdEncoding.EncodeToString(codeResponseBytes))

	var c CodeResponse
	_, err = Codec.Unmarshal(codeResponseBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeResponse.Data, c.Data)
}
