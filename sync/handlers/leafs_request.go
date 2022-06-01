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
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// Maximum number of leaves to return in a message.LeafsResponse
	// This parameter overrides any other Limit specified
	// in message.LeafsRequest if it is greater than this value
	maxLeavesLimit = uint16(1024)

	segmentLen = 64 // divide data from snapshot to segments of this size
)

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
		leafsResponse message.LeafsResponse
		proofTime     time.Duration
		trieReadTime  time.Duration
		more          bool
	)
	defer func() {
		lrh.stats.UpdateGenerateRangeProofTime(proofTime)
		lrh.stats.UpdateReadLeafsTime(trieReadTime)
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
		proof, err := generateRangeProof(t, leafsRequest.Start, snapKeys, more, keyLength, &proofTime)
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
			if err := verifyRangeProof(
				proof, leafsRequest.Root, snapKeys, snapVals, leafsRequest.Start, keyLength, &proofTime,
			); err == nil {
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
		// The data from the snapshot could not be validated as a whole. It is still likely
		// most of the data from the snapshot is useable, so we try to validate smaller
		// segments of the data and use them in the response.
		hasGap := false
		leafsResponse.Keys = make([][]byte, 0, limit)
		leafsResponse.Vals = make([][]byte, 0, limit)
		for i := 0; i < len(snapKeys); i += segmentLen {
			segmentEnd := math.Min(i+segmentLen, len(snapKeys))
			var segmentStartKey []byte
			if i == 0 {
				segmentStartKey = leafsRequest.Start
			} else {
				// We consider a segment valid only if the trie does not contain any
				// newly inserted keys between snapKeys[i-1] and snapKeys[i].
				// To check this, the start key for generating and verifying range proofs
				// is set to the key immediately after snapKeys[i-1].
				segmentStartKey = common.CopyBytes(snapKeys[i-1])
				utils.IncrOne(segmentStartKey)
			}
			proof, err := generateRangeProof(t, segmentStartKey, snapKeys[i:segmentEnd], true, keyLength, &proofTime)
			if err != nil {
				lrh.stats.IncProofError()
				return message.LeafsResponse{}, err
			}
			if err := verifyRangeProof(
				proof, leafsRequest.Root,
				snapKeys[i:segmentEnd], snapVals[i:segmentEnd],
				segmentStartKey, keyLength,
				&proofTime,
			); err != nil {
				// segment is not valid
				lrh.stats.IncSnapshotSegmentInvalid()
				hasGap = true
				continue
			}

			// segment is valid
			lrh.stats.IncSnapshotSegmentValid()
			if hasGap {
				// if there is a gap between valid segments, fill the gap with data from the trie
				_, err := fillFromTrie(ctx, t, &leafsResponse, leafsRequest.Start, snapKeys[i], limit, &trieReadTime)
				if err != nil {
					lrh.stats.IncTrieError()
					return message.LeafsResponse{}, err
				}
				// remove the last key added since it is snapKeys[i] and will be added back
				// Note: this is safe because we were able to verify the range proof that
				// shows snapKeys[i] is part of the trie.
				leafsResponse.Keys = leafsResponse.Keys[:len(leafsResponse.Keys)-1]
				leafsResponse.Vals = leafsResponse.Vals[:len(leafsResponse.Vals)-1]
			}
			hasGap = false
			// all the key/vals in the segment are valid, but possibly shorten segmentEnd
			// here to respect limit
			segmentEnd = math.Min(segmentEnd, i+int(limit)-len(leafsResponse.Keys))
			leafsResponse.Keys = append(leafsResponse.Keys, snapKeys[i:segmentEnd]...)
			leafsResponse.Vals = append(leafsResponse.Vals, snapVals[i:segmentEnd]...)

			if len(leafsResponse.Keys) >= int(limit) {
				break
			}
		}
	} else {
		leafsResponse.Keys = make([][]byte, 0, limit)
		leafsResponse.Vals = make([][]byte, 0, limit)
	}

	if len(leafsResponse.Keys) < int(limit) {
		var err error
		// more indicates whether there are more leaves in the trie
		more, err = fillFromTrie(ctx, t, &leafsResponse, leafsRequest.Start, leafsRequest.End, limit, &trieReadTime)
		if err != nil {
			lrh.stats.IncTrieError()
			return message.LeafsResponse{}, err
		}
	}

	// Generate the proof and add it to the response.
	proof, err := generateRangeProof(t, leafsRequest.Start, leafsResponse.Keys, more, keyLength, &proofTime)
	if err != nil {
		lrh.stats.IncProofError()
		return message.LeafsResponse{}, err
	}
	leafsResponse.ProofKeys, leafsResponse.ProofVals, err = iterateKeyVals(proof)
	if err != nil {
		lrh.stats.IncProofError()
		return message.LeafsResponse{}, err
	}
	return leafsResponse, nil
}

// generateRangeProof generates a range proof for the range specified by [start] and [keys] using [t].
//
// Note: A nil memorydb is returned in the case that the range contains the entire trie, such that no proof is required.
func generateRangeProof(t *trie.Trie, start []byte, keys [][]byte, more bool, keyLength int, duration *time.Duration) (*memorydb.Database, error) {
	startTime := time.Now()
	defer func() { *duration += time.Since(startTime) }()

	// in the case the range covers the whole trie, no proof is required (keys should hash to root)
	if len(start) == 0 && !more {
		return nil, nil
	}

	// If [start] is empty, populate it with the appropriate length key starting at 0.
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

func verifyRangeProof(
	proof *memorydb.Database, root common.Hash, keys, vals [][]byte, start []byte, keyLength int, duration *time.Duration,
) error {
	startTime := time.Now()
	defer func() { *duration += time.Since(startTime) }()

	// If [start] is empty, populate it with the appropriate length key starting at 0.
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

// iterateKeyVals returns the key-value pairs contained in [db]
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

// fillFromTrie iterates key/values from [t], appending them to [leafsResponse.Keys/Values].
// Iteration starts from the key immediately after the last key in [leafsResponse]
// (or [start] if [leafsResponse] is empty) and goes up to [end].
// Returns true if there are more keys in the trie.
func fillFromTrie(
	ctx context.Context,
	t *trie.Trie,
	leafsResponse *message.LeafsResponse,
	start, end []byte,
	limit uint16,
	duration *time.Duration,
) (bool, error) {
	startTime := time.Now()
	defer func() { *duration += time.Since(startTime) }()

	var trieStartKey []byte
	if len(leafsResponse.Keys) > 0 {
		trieStartKey = common.CopyBytes(leafsResponse.Keys[len(leafsResponse.Keys)-1])
		utils.IncrOne(trieStartKey)
	} else {
		trieStartKey = start
	}

	// create iterator to iterate the trie
	it := trie.NewIterator(t.NodeIterator(trieStartKey))
	more := false
	for it.Next() {
		// if we're at the end, break this loop
		if len(end) > 0 && bytes.Compare(it.Key, end) > 0 {
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
	return more, it.Err
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
