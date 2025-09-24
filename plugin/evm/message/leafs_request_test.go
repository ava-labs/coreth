// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

// TestMarshalLeafsRequest requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsRequest(t *testing.T) {
	rand := rand.New(rand.NewSource(1)) //nolint:gosec

	startBytes := make([]byte, common.HashLength)
	endBytes := make([]byte, common.HashLength)

	_, err := rand.Read(startBytes)
	require.NoError(t, err)

	_, err = rand.Read(endBytes)
	require.NoError(t, err)

	leafsRequest := LeafsRequest{
		Root:     common.BytesToHash([]byte("im ROOTing for ya")),
		Start:    startBytes,
		End:      endBytes,
		Limit:    1024,
		NodeType: StateTrieNode,
	}

	base64LeafsRequest := "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIJKwrrLuvEv+pCsRmxCs6C3pT+zQMou38OjnrYUylszpAAAAILaro5a21oPenq7+x6aZ3RSpIQkoga9KNpz82H8eyrV5BAAB"

	leafsRequestBytes, err := Codec.Marshal(Version, leafsRequest)
	require.NoError(t, err)
	require.Equal(t, base64LeafsRequest, base64.StdEncoding.EncodeToString(leafsRequestBytes))

	var l LeafsRequest
	_, err = Codec.Unmarshal(leafsRequestBytes, &l)
	require.NoError(t, err)
	require.Equal(t, leafsRequest.Root, l.Root)
	require.Equal(t, leafsRequest.Start, l.Start)
	require.Equal(t, leafsRequest.End, l.End)
	require.Equal(t, leafsRequest.Limit, l.Limit)
	require.Equal(t, leafsRequest.NodeType, l.NodeType)
}

// TestMarshalLeafsResponse requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsResponse(t *testing.T) {
	rand := rand.New(rand.NewSource(1)) //nolint:gosec

	keysBytes := make([][]byte, 16)
	valsBytes := make([][]byte, 16)
	for i := range keysBytes {
		keysBytes[i] = make([]byte, common.HashLength)
		valsBytes[i] = make([]byte, rand.Intn(8)+8) // min 8 bytes, max 16 bytes

		_, err := rand.Read(keysBytes[i])
		require.NoError(t, err)
		_, err = rand.Read(valsBytes[i])
		require.NoError(t, err)
	}

	nextKey := make([]byte, common.HashLength)
	_, err := rand.Read(nextKey)
	require.NoError(t, err)

	proofVals := make([][]byte, 4)
	for i := range proofVals {
		proofVals[i] = make([]byte, rand.Intn(8)+8) // min 8 bytes, max 16 bytes

		_, err = rand.Read(proofVals[i])
		require.NoError(t, err)
	}

	leafsResponse := LeafsResponse{
		Keys:      keysBytes,
		Vals:      valsBytes,
		More:      true,
		ProofVals: proofVals,
	}

	base64LeafsResponse := "AAAAAAAQAAAAIHmj1vjEoZ0O4ePA2DhZKO+Ploa4q7gLlGaVGRnGiAr5AAAAIPlMwaMaBQeLkZP1M+KCqHN+KVIikeZA0b41XPhooHmOAAAAIG4lzYfLwAAcjQqoSKiyos2SbCuv9L6KJqkRd+sZWz4AAAAAIHzxct9NSipNR3HnZ8GG8tqPnfRdx0z5/sbbf7syDAuwAAAAINUOE8de3g9LiexMNfeGpq9fgWOLeR85nQfbce+/paipAAAAIGGVdxIl3EC71j8yrssykJj0aW2AzCGNllzGQiIUHVxUAAAAIM21WR+aC1oC/Y0aF7MtirZbCPHn6/E1Vc2SqdMetCkSAAAAIF0iMl4/jN1BZVWE2vhHPLhNf1GU7ZG3Ha/hRlfgKDEmAAAAINWU8iL34iUskB3aDoXzcFnIx+GC1dOaTb6U67yPDYvUAAAAIA3fhS/DRGIMmSipCgkPvsf9wWmmFdj/sqcuNTfbajgJAAAAIIgDEuKc5UMEOSk5EK1R5Cqb2ZSIeP0A3EwWRBLNyoUcAAAAIMc57Tz4uWzF/SNzh/Atj9mVgINDHNAtTI8q544m1X47AAAAIOMYINGZvfG5FkaMfFlmU9YH6iTkno0XYJRdzz5wmzvkAAAAIGxx030ahNis5MO4BKmrebrSM66owbYrvOh2aC4wXBHsAAAAIPy2H9p6M+09QiQ5LJEQ/t5aUiWu4JYay996TX970dbKAAAAICk/c3GuBZW6aaj1JOGJ0TV7XJlzyY5wdyRMph1Pvu88AAAAEAAAAAheRm4y4L4/egAAAAl4BX68DVmqf5oAAAAK28t5TDoKXe3mTwAAAAvU000vwEklyciOvwAAAAyMpkqLoo9xc7t3/c4AAAANkNAZCfHhlgAL7dUnAwAAAA5TIzp83z0wYkVwNOxi8QAAAA9+g6PBPoROJ4rj5NnchCgAAAAQDh3Z3uqi4VpY/dqO6Q4pRwAAAAjECTPaIxeFOgAAAAnol1UASNalh6AAAAAKxZ3p1yeNLQ875QAAAAsv1HBF3rWoiq5YYwAAAAyOBRehPDv4UGOK1cMAAAANqykJIIyZKGT8Ag2FTwAAAA4CEC5Xar3pHA0bb0cDCQAAAAQAAAAIOX+OwhaAm/YAAAAJ3JVfblHw6v/oAAAACp0GQdu6CmNquyYAAAAL/FNLguGJX8xGQ7Q="

	leafsResponseBytes, err := Codec.Marshal(Version, leafsResponse)
	require.NoError(t, err)
	require.Equal(t, base64LeafsResponse, base64.StdEncoding.EncodeToString(leafsResponseBytes))

	var l LeafsResponse
	_, err = Codec.Unmarshal(leafsResponseBytes, &l)
	require.NoError(t, err)
	require.Equal(t, leafsResponse.Keys, l.Keys)
	require.Equal(t, leafsResponse.Vals, l.Vals)
	require.False(t, l.More) // make sure it is not serialized
	require.Equal(t, leafsResponse.ProofVals, l.ProofVals)
}
