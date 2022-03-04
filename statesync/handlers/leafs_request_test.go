// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/ava-labs/coreth/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestLeafsRequestHandler_OnLeafsRequest(t *testing.T) {
	codec, err := message.BuildCodec()
	if err != nil {
		t.Fatal("unexpected error building codec", err)
	}

	mockHandlerStats := &stats.MockHandlerStats{}
	memdb := memorydb.New()
	trieDB := trie.NewDatabase(memdb)

	largeTrieRoot, largeTrieKeys, _ := trie.GenerateTrie(t, trieDB, 10_000)
	smallTrieRoot, _, _ := trie.GenerateTrie(t, trieDB, 500)

	leafsHandler := NewLeafsRequestHandler(trieDB, mockHandlerStats, codec)

	tests := map[string]struct {
		prepareTestFn    func() (context.Context, message.LeafsRequest)
		assertResponseFn func(*testing.T, message.LeafsRequest, []byte, error)
	}{
		"zero limit dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    0,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty storage root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     types.EmptyRootHash,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"missing root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     common.BytesToHash([]byte("something is missing here...")),
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.MissingRootCount)
			},
		},
		"cancelled context dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
			},
		},
		"nil start and end range dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    nil,
					End:      nil,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"nil end range dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      nil,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"end greater than start dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xbb}, common.HashLength),
					End:      bytes.Repeat([]byte{0xaa}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"invalid node type dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xbb}, common.HashLength),
					End:      bytes.Repeat([]byte{0xaa}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.NodeType(11),
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
			},
		},
		"max leaves overridden": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit * 10,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)
			},
		},
		"full range with nil start": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    nil,
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, bytes.Repeat([]byte{0x00}, common.HashLength), leafsResponse.Keys[len(leafsResponse.Keys)-1], leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"full range with 0x00 start": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, bytes.Repeat([]byte{0x00}, common.HashLength), leafsResponse.Keys[len(leafsResponse.Keys)-1], leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"partial mid range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				startKey := largeTrieKeys[1_000]
				startKey[31] = startKey[31] + 1
				endKey := largeTrieKeys[1_040]
				endKey[31] = endKey[31] - 1
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    startKey,
					End:      endKey,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, 40, len(leafsResponse.Keys))
				assert.EqualValues(t, 40, len(leafsResponse.Vals))
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"partial end range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    largeTrieKeys[9_400],
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, 600, len(leafsResponse.Keys))
				assert.EqualValues(t, 600, len(leafsResponse.Vals))
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.False(t, more)
			},
		},
		"small trie root": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    nil,
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NotEmpty(t, response)

				var leafsResponse message.LeafsResponse
				if _, err = codec.Unmarshal(response, &leafsResponse); err != nil {
					t.Fatalf("unexpected error when unmarshalling LeafsResponse: %v", err)
				}

				assert.EqualValues(t, 500, len(leafsResponse.Keys))
				assert.EqualValues(t, 500, len(leafsResponse.Vals))
				assert.Empty(t, leafsResponse.ProofKeys)
				assert.Empty(t, leafsResponse.ProofVals)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				firstKey := bytes.Repeat([]byte{0x00}, common.HashLength)
				lastKey := leafsResponse.Keys[len(leafsResponse.Keys)-1]

				more, err := trie.VerifyRangeProof(smallTrieRoot, firstKey, lastKey, leafsResponse.Keys, leafsResponse.Vals, nil)
				assert.NoError(t, err)
				assert.False(t, more)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, request := test.prepareTestFn()
			response, err := leafsHandler.OnLeafsRequest(ctx, ids.GenerateTestShortID(), 1, request)
			test.assertResponseFn(t, request, response, err)
			mockHandlerStats.Reset()
		})
	}
}

func TestLeafsRequestHandlerSmallTrie(t *testing.T) {
	codec, err := message.BuildCodec()
	if err != nil {
		t.Fatal("unexpected error building codec", err)
	}

	mockHandlerStats := &stats.MockHandlerStats{}
	memdb := memorydb.New()
	trieDB := trie.NewDatabase(memdb)
	tr, err := trie.New(common.Hash{}, trieDB)
	assert.NoError(t, err)

	// set random seed to ensure consistent data every time
	rand.Seed(1)

	// randomly generate some leaf data
	for i := 0; i < 500; i++ {
		data := make([]byte, rand.Intn(32)+32)
		if _, err = rand.Read(data); err != nil {
			t.Fatal("error reading random bytes", err)
		}

		key := crypto.Keccak256Hash(data)
		assert.NoError(t, tr.TryUpdate(key[:], data))
	}

	// commit the trie
	root, _, err := tr.Commit(nil)
	if err != nil {
		t.Fatal("could not commit trie", err)
	}

	// commit trie DB
	if err = trieDB.Commit(root, false, nil); err != nil {
		t.Fatal("error committing trieDB", err)
	}

	leafsHandler := NewLeafsRequestHandler(trieDB, mockHandlerStats, codec)

	responseBytes, err := leafsHandler.OnLeafsRequest(context.Background(), ids.GenerateTestShortID(), 1, message.LeafsRequest{
		Root:     root,
		Start:    nil,
		End:      bytes.Repeat([]byte{0xff}, common.HashLength),
		Limit:    maxLeavesLimit,
		NodeType: message.StateTrieNode,
	})

	if err != nil {
		t.Fatalf("unexpected error when calling LeafsRequestHandler: %v", err)
	}

	assert.NotEmpty(t, responseBytes)

	var leafsResponse message.LeafsResponse
	if _, err = codec.Unmarshal(responseBytes, &leafsResponse); err != nil {
		t.Fatalf("unexpected error when unmarshalling LeafsResponse: %v", err)
	}

	assert.EqualValues(t, 500, len(leafsResponse.Keys))
	assert.EqualValues(t, 500, len(leafsResponse.Vals))
	assert.Empty(t, leafsResponse.ProofKeys)
	assert.Empty(t, leafsResponse.ProofVals)
	assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
	assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

	firstKey := bytes.Repeat([]byte{0x00}, common.HashLength)
	lastKey := leafsResponse.Keys[len(leafsResponse.Keys)-1]

	more, err := trie.VerifyRangeProof(root, firstKey, lastKey, leafsResponse.Keys, leafsResponse.Vals, nil)
	assert.NoError(t, err)
	assert.False(t, more)
}
