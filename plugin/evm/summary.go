// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// syncableBlockSummary is a basic implementation of common.Summary
// used in syncableBlockToSummary to convert message.SyncableBlock
// to common.Summary
type syncableBlockSummary struct {
	bytes []byte
	key   uint64
	id    ids.ID
}

var _ common.Summary = &syncableBlockSummary{}

func (s *syncableBlockSummary) Bytes() []byte { return s.bytes }
func (s *syncableBlockSummary) Key() uint64   { return s.key }
func (s *syncableBlockSummary) ID() ids.ID    { return s.id }

// syncableBlockToSummary builds common.Summary given a message.SyncableBlock
// Marshals message.SyncableBlock as Content in Summary
// Returns error if failed to marshal.
func syncableBlockToSummary(codec codec.Manager, syncableBlock message.SyncableBlock) (common.Summary, error) {
	contentBytes, err := codec.Marshal(message.Version, syncableBlock)
	if err != nil {
		return nil, err
	}

	summaryID, err := ids.ToID(crypto.Keccak256(contentBytes))
	return &syncableBlockSummary{
		key:   syncableBlock.BlockNumber,
		id:    summaryID,
		bytes: contentBytes,
	}, err
}

// parseSummary parses summaryBytes into a message.SyncableBlock
func parseSummary(codec codec.Manager, summaryBytes []byte) (message.SyncableBlock, error) {
	var syncableBlock message.SyncableBlock
	if _, err := codec.Unmarshal(summaryBytes, &syncableBlock); err != nil {
		return message.SyncableBlock{}, err
	}

	return syncableBlock, nil
}
