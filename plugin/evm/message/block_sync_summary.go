// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ Syncable = (*BlockSyncSummary)(nil)

// codecWithBlockSync is the codec manager that contains the codec for BlockSyncSummary and
// other message types that are used in the network protocol. This is to ensure that the codec
// version is consistent across all message types and includes the codec for BlockSyncSummary.
var codecWithBlockSync codec.Manager

func init() {
	var err error
	codecWithBlockSync, err = NewCodec(BlockSyncSummary{})
	if err != nil {
		panic(fmt.Errorf("failed to create codec manager: %w", err))
	}
}

// BlockSyncSummary provides the information necessary to sync a node starting
// at the given block.
type BlockSyncSummary struct {
	BlockNumber uint64      `serialize:"true"`
	BlockHash   common.Hash `serialize:"true"`
	BlockRoot   common.Hash `serialize:"true"`

	summaryID  ids.ID
	bytes      []byte
	acceptImpl AcceptImplFn
}

type BlockSyncSummaryParser struct{}

func NewBlockSyncSummaryParser() *BlockSyncSummaryParser {
	return &BlockSyncSummaryParser{}
}

func (b *BlockSyncSummaryParser) ParseFromBytes(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error) {
	summary := BlockSyncSummary{}
	if codecVersion, err := codecWithBlockSync.Unmarshal(summaryBytes, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse syncable summary: %w", err)
	} else if codecVersion != Version {
		return nil, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (%d != %d)", codecVersion, Version)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to compute summary ID: %w", err)
	}
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return &summary, nil
}

func NewBlockSyncSummary(blockHash common.Hash, blockNumber uint64, blockRoot common.Hash) (*BlockSyncSummary, error) {
	summary := BlockSyncSummary{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		BlockRoot:   blockRoot,
	}
	bytes, err := codecWithBlockSync.Marshal(Version, &summary)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal syncable summary: %w", err)
	}

	summary.bytes = bytes
	summaryID, err := ids.ToID(crypto.Keccak256(bytes))
	if err != nil {
		return nil, fmt.Errorf("failed to compute summary ID: %w", err)
	}
	summary.summaryID = summaryID

	return &summary, nil
}

func (s *BlockSyncSummary) GetBlockHash() common.Hash {
	return s.BlockHash
}

func (s *BlockSyncSummary) GetBlockRoot() common.Hash {
	return s.BlockRoot
}

func (s *BlockSyncSummary) Bytes() []byte {
	return s.bytes
}

func (s *BlockSyncSummary) Height() uint64 {
	return s.BlockNumber
}

func (s *BlockSyncSummary) ID() ids.ID {
	return s.summaryID
}

func (s *BlockSyncSummary) String() string {
	return fmt.Sprintf("BlockSyncSummary(BlockHash=%s, BlockNumber=%d, BlockRoot=%s)", s.BlockHash, s.BlockNumber, s.BlockRoot)
}

func (s *BlockSyncSummary) Accept(context.Context) (block.StateSyncMode, error) {
	if s.acceptImpl == nil {
		return block.StateSyncSkipped, fmt.Errorf("accept implementation not specified for summary: %s", s)
	}
	return s.acceptImpl(s)
}
