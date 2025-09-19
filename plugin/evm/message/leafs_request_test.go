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

// TestMarshalLeafsRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsRequest(t *testing.T) {
	startBytes := make([]byte, common.HashLength)
	endBytes := make([]byte, common.HashLength)

	copy(startBytes, deterministicBytes("leafs-start", common.HashLength))
	copy(endBytes, deterministicBytes("leafs-end", common.HashLength))

	leafsRequest := LeafsRequest{
		Root:     common.BytesToHash([]byte("im ROOTing for ya")),
		Start:    startBytes,
		End:      endBytes,
		Limit:    1024,
		NodeType: StateTrieNode,
	}

	base64LeafsRequest := "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIJKwrrLuvEv+pCsRmxCs6C3pT+zQMou38OjnrYUylszpAAAAILaro5a21oPenq7+x6aZ3RSpIQkoga9KNpz82H8eyrV5BAAB"

	leafsRequestBytes, err := Codec.Marshal(Version, leafsRequest)
	assert.NoError(t, err)
	encoded := base64.StdEncoding.EncodeToString(leafsRequestBytes)
	t.Log("LeafsRequest base64:", encoded)
	assert.Equal(t, base64LeafsRequest, encoded)

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
		size := 8 + (i % 9)
		valsBytes[i] = make([]byte, size)

		copy(keysBytes[i], deterministicBytes(fmt.Sprintf("leafs-key-%d", i), common.HashLength))
		copy(valsBytes[i], deterministicBytes(fmt.Sprintf("leafs-val-%d", i), size))
	}

	nextKey := make([]byte, common.HashLength)
	copy(nextKey, deterministicBytes("leafs-next-key", common.HashLength))

	proofVals := make([][]byte, 4)
	for i := range proofVals {
		size := 8 + (i % 9)
		proofVals[i] = make([]byte, size)

		copy(proofVals[i], deterministicBytes(fmt.Sprintf("leafs-proof-%d", i), size))
	}

	leafsResponse := LeafsResponse{
		Keys:      keysBytes,
		Vals:      valsBytes,
		More:      true,
		ProofVals: proofVals,
	}

	base64LeafsResponse := "AAAAAAAQAAAAIHmj1vjEoZ0O4ePA2DhZKO+Ploa4q7gLlGaVGRnGiAr5AAAAIPlMwaMaBQeLkZP1M+KCqHN+KVIikeZA0b41XPhooHmOAAAAIG4lzYfLwAAcjQqoSKiyos2SbCuv9L6KJqkRd+sZWz4AAAAAIHzxct9NSipNR3HnZ8GG8tqPnfRdx0z5/sbbf7syDAuwAAAAINUOE8de3g9LiexMNfeGpq9fgWOLeR85nQfbce+/paipAAAAIGGVdxIl3EC71j8yrssykJj0aW2AzCGNllzGQiIUHVxUAAAAIM21WR+aC1oC/Y0aF7MtirZbCPHn6/E1Vc2SqdMetCkSAAAAIF0iMl4/jN1BZVWE2vhHPLhNf1GU7ZG3Ha/hRlfgKDEmAAAAINWU8iL34iUskB3aDoXzcFnIx+GC1dOaTb6U67yPDYvUAAAAIA3fhS/DRGIMmSipCgkPvsf9wWmmFdj/sqcuNTfbajgJAAAAIIgDEuKc5UMEOSk5EK1R5Cqb2ZSIeP0A3EwWRBLNyoUcAAAAIMc57Tz4uWzF/SNzh/Atj9mVgINDHNAtTI8q544m1X47AAAAIOMYINGZvfG5FkaMfFlmU9YH6iTkno0XYJRdzz5wmzvkAAAAIGxx030ahNis5MO4BKmrebrSM66owbYrvOh2aC4wXBHsAAAAIPy2H9p6M+09QiQ5LJEQ/t5aUiWu4JYay996TX970dbKAAAAICk/c3GuBZW6aaj1JOGJ0TV7XJlzyY5wdyRMph1Pvu88AAAAEAAAAAheRm4y4L4/egAAAAl4BX68DVmqf5oAAAAK28t5TDoKXe3mTwAAAAvU000vwEklyciOvwAAAAyMpkqLoo9xc7t3/c4AAAANkNAZCfHhlgAL7dUnAwAAAA5TIzp83z0wYkVwNOxi8QAAAA9+g6PBPoROJ4rj5NnchCgAAAAQDh3Z3uqi4VpY/dqO6Q4pRwAAAAjECTPaIxeFOgAAAAnol1UASNalh6AAAAAKxZ3p1yeNLQ875QAAAAsv1HBF3rWoiq5YYwAAAAyOBRehPDv4UGOK1cMAAAANqykJIIyZKGT8Ag2FTwAAAA4CEC5Xar3pHA0bb0cDCQAAAAQAAAAIOX+OwhaAm/YAAAAJ3JVfblHw6v/oAAAACp0GQdu6CmNquyYAAAAL/FNLguGJX8xGQ7Q="

	leafsResponseBytes, err := Codec.Marshal(Version, leafsResponse)
	assert.NoError(t, err)
	encoded := base64.StdEncoding.EncodeToString(leafsResponseBytes)
	t.Log("LeafsResponse base64:", encoded)
	assert.Equal(t, base64LeafsResponse, encoded)

	var l LeafsResponse
	_, err = Codec.Unmarshal(leafsResponseBytes, &l)
	assert.NoError(t, err)
	assert.Equal(t, leafsResponse.Keys, l.Keys)
	assert.Equal(t, leafsResponse.Vals, l.Vals)
	assert.False(t, l.More) // make sure it is not serialized
	assert.Equal(t, leafsResponse.ProofVals, l.ProofVals)
}
