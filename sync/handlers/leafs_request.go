// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// Maximum number of leaves to return in a message.LeafsResponse
// This parameter overrides any other Limit specified
// in message.LeafsRequest if it is greater than this value
const maxLeavesLimit = uint16(1024)

// LeafsRequestHandler is a peer.RequestHandler for types.LeafsRequest
// serving requested trie data
type LeafsRequestHandler struct {
	trieDB *trie.Database
	codec  codec.Manager
	stats  stats.LeafsRequestHandlerStats
}

func NewLeafsRequestHandler(trieDB *trie.Database, codec codec.Manager, syncerStats stats.LeafsRequestHandlerStats) *LeafsRequestHandler {
	return &LeafsRequestHandler{
		trieDB: trieDB,
		codec:  codec,
		stats:  syncerStats,
	}
}

// OnLeafsRequest returns encoded message.LeafsResponse for a given message.LeafsRequest
// Returns leaves with proofs for specified (Start-End) (both inclusive) ranges
// Returned message.LeafsResponse may contain partial leaves within requested Start and End range if:
// - ctx expired while fetching leafs
// - number of leaves read is greater than Limit (message.LeafsRequest)
// Specified Limit in message.LeafsRequest is overridden to maxLeavesLimit if it is greater than maxLeavesLimit
// Expects returned errors to be treated as FATAL
// Never returns errors
// Expects NodeType to be one of message.AtomicTrieNode or message.StateTrieNode
// Returns nothing if NodeType is invalid or requested trie root is not found
// Assumes ctx is active
func (lrh *LeafsRequestHandler) OnLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	startTime := time.Now()
	lrh.stats.IncLeafsRequest()

	if (len(leafsRequest.End) > 0 && bytes.Compare(leafsRequest.Start, leafsRequest.End) > 0) ||
		leafsRequest.Root == (common.Hash{}) ||
		leafsRequest.Root == types.EmptyRootHash ||
		leafsRequest.Limit == 0 {
		log.Debug("invalid leafs request, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}
	keyLength, err := getKeyLength(leafsRequest.NodeType)
	if err != nil {
		// Note: LeafsRequest.Handle checks NodeType's validity so clients cannot cause the server to spam this error
		log.Error("Failed to get key length for leafs request", "err", err)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}
	if len(leafsRequest.Start) != 0 && len(leafsRequest.Start) != keyLength ||
		len(leafsRequest.End) != 0 && len(leafsRequest.End) != keyLength {
		log.Debug("invalid length for leafs request range, dropping request", "startLen", len(leafsRequest.Start), "endLen", len(leafsRequest.End), "expected", keyLength)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}

	t, err := trie.New(leafsRequest.Root, lrh.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping request", "nodeID", nodeID, "requestID", requestID, "root", leafsRequest.Root, "err", err)
		lrh.stats.IncMissingRoot()
		return nil, nil
	}

	leafsResponse, err := lrh.handleRequest(ctx, leafsRequest, t, keyLength)
	// ensure metrics are captured properly on all return paths
	defer func() {
		lrh.stats.UpdateLeafsRequestProcessingTime(time.Since(startTime))
		lrh.stats.UpdateLeafsReturned(uint16(len(leafsResponse.Keys)))
		lrh.stats.UpdateRangeProofKeysReturned(int64(len(leafsResponse.ProofKeys)))
	}()
	if err != nil {
		log.Debug("failed to serve leafs request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}
	if len(leafsResponse.Keys) == 0 && ctx.Err() != nil {
		log.Debug("context err set before any leafs were iterated", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "ctxErr", ctx.Err())
		return nil, nil
	}

	responseBytes, err := lrh.codec.Marshal(message.Version, leafsResponse)
	if err != nil {
		log.Debug("failed to marshal LeafsResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}

	log.Debug("handled leafsRequest", "time", time.Since(startTime), "leafs", len(leafsResponse.Keys), "proofLen", len(leafsResponse.ProofKeys))
	return responseBytes, nil
}

func (lrh *LeafsRequestHandler) handleRequest(
	ctx context.Context, leafsRequest message.LeafsRequest, t *trie.Trie, keyLength int,
) (message.LeafsResponse, error) {
	var (
		leafsResponse  message.LeafsResponse
		rangeProofTime time.Duration
	)
	defer func() {
		lrh.stats.UpdateGenerateRangeProofTime(rangeProofTime)
	}()

	// override limit if it is greater than the configured maxLeavesLimit
	limit := leafsRequest.Limit
	if limit > maxLeavesLimit {
		limit = maxLeavesLimit
	}

	// Reading from snapshot is only applicable to state trie
	if leafsRequest.NodeType == message.StateTrieNode {
		snapshotReadStart := time.Now()
		lrh.stats.IncSnapshotReadAttempt()

		// Get an iterator into the account snapshot or the storage snapshot
		var snapIt ethdb.Iterator
		diskDB := lrh.trieDB.DiskDB()
		if leafsRequest.Account == (common.Hash{}) {
			snapIt = snapshot.NewAccountSnapshotIterator(diskDB, leafsRequest.Start, leafsRequest.End)
		} else {
			snapIt = snapshot.NewStorageSnapshotIterator(diskDB, leafsRequest.Account, leafsRequest.Start, leafsRequest.End)
		}
		defer snapIt.Release()

		// Optimistically read leafs from the snapshot, assuming they have not been
		// modified since the requested root. If this assumption can be verified with
		// range proofs and data from the trie, we can skip iterating the trie as
		// an optimization.
		more := false
		snapKeys := make([][]byte, 0, limit)
		snapVals := make([][]byte, 0, limit)
		for snapIt.Next() {
			// if we're at the end, break this loop
			if len(leafsRequest.End) > 0 && bytes.Compare(snapIt.Key(), leafsRequest.End) > 0 {
				more = true
				break
			}
			// If we've returned enough data or run out of time, set the more flag and exit
			// this flag will determine if the proof is generated or not
			if len(snapKeys) >= int(limit) || ctx.Err() != nil {
				more = true
				break
			}

			snapKeys = append(snapKeys, snapIt.Key())
			snapVals = append(snapVals, snapIt.Value())
		}
		// Update read snapshot time here, so that we include the case that an error occurred.
		lrh.stats.UpdateSnapshotReadTime(time.Since(snapshotReadStart))
		if err := snapIt.Error(); err != nil {
			lrh.stats.IncSnapshotReadError()
			return message.LeafsResponse{}, err
		}

		// Check if the entire range read from the snapshot is valid according to the trie.
		rangeProofStart := time.Now()
		proof, err := generateRangeProof(t, snapKeys, leafsRequest.Start, more, keyLength)
		rangeProofTime += time.Since(rangeProofStart)
		if err != nil {
			lrh.stats.IncProofError()
			return message.LeafsResponse{}, err
		}
		if proof == nil {
			// check if the root in the snapshot matches the request, in this case we expect
			// the leafs to hash back to the root so there is no need to verify it.
			if snapRoot, err := getSnapshotRoot(diskDB, leafsRequest.Account); err != nil {
				lrh.stats.IncSnapshotReadError()
				return message.LeafsResponse{}, err
			} else if snapRoot == leafsRequest.Root {
				// success
				leafsResponse.Keys, leafsResponse.Vals = snapKeys, snapVals
				lrh.stats.IncSnapshotReadSuccess()
				return leafsResponse, nil
			}
		} else {
			verifyRangeProofStart := time.Now()
			err := verifyRangeProof(proof, leafsRequest.Root, snapKeys, snapVals, leafsRequest.Start, keyLength)
			rangeProofTime += time.Since(verifyRangeProofStart)
			if err == nil {
				// success
				leafsResponse.Keys, leafsResponse.Vals = snapKeys, snapVals
				leafsResponse.ProofKeys, leafsResponse.ProofVals, err = iterateKeyVals(proof)
				if err != nil {
					lrh.stats.IncProofError()
					return message.LeafsResponse{}, err
				}
				lrh.stats.IncSnapshotReadSuccess()
				return leafsResponse, nil
			}
		}
	}

	// create iterator to iterate the trie
	// Note leafsRequest.Start could be empty or it could be the next key
	// from a partial response to a previous request
	readFromTrieStart := time.Now()
	it := trie.NewIterator(t.NodeIterator(leafsRequest.Start))

	// more indicates whether there are more leaves in the trie
	more := false
	leafsResponse.Keys = make([][]byte, 0, limit)
	leafsResponse.Vals = make([][]byte, 0, limit)
	for it.Next() {
		// if we're at the end, break this loop
		if len(leafsRequest.End) > 0 && bytes.Compare(it.Key, leafsRequest.End) > 0 {
			more = true
			break
		}

		// If we've returned enough data or run out of time, set the more flag and exit
		// this flag will determine if the proof is generated or not
		if len(leafsResponse.Keys) >= int(limit) || ctx.Err() != nil {
			more = true
			break
		}

		// collect data to return
		leafsResponse.Keys = append(leafsResponse.Keys, it.Key)
		leafsResponse.Vals = append(leafsResponse.Vals, it.Value)
	}
	// Update read leafs time here, so that we include the case that an error occurred.
	lrh.stats.UpdateReadLeafsTime(time.Since(readFromTrieStart))

	if it.Err != nil {
		lrh.stats.IncTrieError()
		return message.LeafsResponse{}, it.Err
	}

	// Generate the proof and add it to the response.
	rangeProofStart := time.Now()
	proof, err := generateRangeProof(t, leafsResponse.Keys, leafsRequest.Start, more, keyLength)
	if err != nil {
		rangeProofTime += time.Since(rangeProofStart)
		lrh.stats.IncProofError()
		return message.LeafsResponse{}, err
	}
	leafsResponse.ProofKeys, leafsResponse.ProofVals, err = iterateKeyVals(proof)
	rangeProofTime += time.Since(rangeProofStart)
	if err != nil {
		lrh.stats.IncProofError()
		return message.LeafsResponse{}, err
	}

	return leafsResponse, nil
}

// generateRangeProof generates a range proof for the range specified by [start] and [keys] using [t].
func generateRangeProof(t *trie.Trie, keys [][]byte, start []byte, more bool, keyLength int) (*memorydb.Database, error) {
	// in the case the range covers the whole trie, no proof is required (keys should hash to root)
	if len(start) == 0 && !more {
		return nil, nil
	}

	// If [start] in the request is empty, populate it with the appropriate length
	// key starting at 0.
	if len(start) == 0 {
		start = bytes.Repeat([]byte{0x00}, keyLength)
	}

	proof := memorydb.New()
	if err := t.Prove(start, 0, proof); err != nil {
		return nil, err
	}
	// If there is a non-zero number of keys, set [end] for the range proof to the
	// last key included in the response.
	if len(keys) > 0 {
		end := keys[len(keys)-1]
		if err := t.Prove(end, 0, proof); err != nil {
			return nil, err
		}
	}
	return proof, nil
}

func verifyRangeProof(proof *memorydb.Database, root common.Hash, keys, vals [][]byte, start []byte, keyLength int) error {
	// If [start] in the request is empty, populate it with the appropriate length
	// key starting at 0.
	if len(start) == 0 {
		start = bytes.Repeat([]byte{0x00}, keyLength)
	}
	var end []byte
	if len(keys) > 0 {
		end = keys[len(keys)-1]
	}
	_, err := trie.VerifyRangeProof(root, start, end, keys, vals, proof)
	return err
}

func iterateKeyVals(db *memorydb.Database) ([][]byte, [][]byte, error) {
	if db == nil {
		return nil, nil, nil
	}
	// iterate db into [][]byte and return
	it := db.NewIterator(nil, nil)
	defer it.Release()

	keys := make([][]byte, 0, db.Len())
	vals := make([][]byte, 0, db.Len())
	for it.Next() {
		keys = append(keys, it.Key())
		vals = append(vals, it.Value())
	}

	return keys, vals, it.Error()
}

// getKeyLength returns trie key length for given nodeType
// expects nodeType to be one of message.AtomicTrieNode or message.StateTrieNode
func getKeyLength(nodeType message.NodeType) (int, error) {
	switch nodeType {
	case message.AtomicTrieNode:
		return wrappers.LongLen + common.HashLength, nil
	case message.StateTrieNode:
		return common.HashLength, nil
	}
	return 0, fmt.Errorf("cannot get key length for unknown node type: %s", nodeType)
}

// getSnapshotRoot returns the root of the storage trie for [account], and
// the root of the main account trie if [account] is empty.
func getSnapshotRoot(db ethdb.KeyValueReader, account common.Hash) (common.Hash, error) {
	if account == (common.Hash{}) {
		return rawdb.ReadSnapshotRoot(db), nil
	}
	var acc snapshot.Account
	accBytes := rawdb.ReadAccountSnapshot(db, account)
	if len(accBytes) == 0 {
		return common.Hash{}, nil
	}
	if err := rlp.DecodeBytes(accBytes, &acc); err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(acc.Root), nil
}
