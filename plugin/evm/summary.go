// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// syncableBlockToSummary builds common.Summary given a message.SyncableBlock
// Marshals message.SyncableBlock as Content in Summary
// Returns error if failed to marshal.
func syncableBlockToSummary(codec codec.Manager, syncableBlock message.SyncableBlock) (common.Summary, error) {
	contentBytes, err := codec.Marshal(message.Version, syncableBlock)
	if err != nil {
		return nil, err
	}

	// engine needs to access block ID and height,
	// so we use this intermediary struct.
	engineSummary := block.CoreSummaryContent{
		BlkID:   ids.ID(syncableBlock.BlockHash),
		Height:  syncableBlock.BlockNumber,
		Content: contentBytes,
	}
	// engineSummary needs to be deserialized by the engine, so we use the same version
	// as the engine does in the codec here
	summary, err := codec.Marshal(block.StateSyncDefaultKeysVersion, engineSummary)
	if err != nil {
		return nil, err
	}

	summaryID, err := ids.ToID(crypto.Keccak256(summary))
	return &block.Summary{
		SummaryKey:   common.SummaryKey(engineSummary.Height),
		SummaryID:    common.SummaryID(summaryID),
		ContentBytes: summary,
	}, err
}

// parseSummary parses common.Summary into a message.SyncableBlock
func parseSummary(codec codec.Manager, summaryBytes []byte) (message.SyncableBlock, error) {
	var engineSummary block.CoreSummaryContent
	if _, err := codec.Unmarshal(summaryBytes, &engineSummary); err != nil {
		return message.SyncableBlock{}, err
	}

	// TODO: currently encodes height and block ID twice.
	var syncableBlock message.SyncableBlock
	if _, err := codec.Unmarshal(engineSummary.Content, &syncableBlock); err != nil {
		return message.SyncableBlock{}, err
	}

	return syncableBlock, nil
}
