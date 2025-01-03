// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ Syncable = &BlockSyncSummary{}

type Syncable interface {
	block.StateSummary
	GetBlockNumber() uint64
	GetBlockHash() common.Hash
	GetBlockRoot() common.Hash
}

type SyncableParser interface {
	ParseFromBytes(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error)
}

type AcceptImplFn func(Syncable) (block.StateSyncMode, error)

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

func NewBlockSyncSummaryParser() SyncableParser {
	return &BlockSyncSummaryParser{}
}

func (b *BlockSyncSummaryParser) ParseFromBytes(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error) {
	summary := BlockSyncSummary{}
	if codecVersion, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
		return nil, err
	} else if codecVersion != Version {
		return nil, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (%d != %d)", codecVersion, Version)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	if err != nil {
		return nil, err
	}
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return &summary, nil
}

func NewBlockSyncSummary(blockHash common.Hash, blockNumber uint64, blockRoot common.Hash) (Syncable, error) {
	summary := BlockSyncSummary{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		BlockRoot:   blockRoot,
	}
	bytes, err := Codec.Marshal(Version, &summary)
	if err != nil {
		return nil, err
	}

	summary.bytes = bytes
	summaryID, err := ids.ToID(crypto.Keccak256(bytes))
	if err != nil {
		return nil, err
	}
	summary.summaryID = summaryID

	return &summary, nil
}

func (s *BlockSyncSummary) GetBlockNumber() uint64 {
	return s.BlockNumber
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
	return fmt.Sprintf("SyncSummary(BlockHash=%s, BlockNumber=%d, BlockRoot=%s)", s.BlockHash, s.BlockNumber, s.BlockRoot)
}

func (s *BlockSyncSummary) Accept(context.Context) (block.StateSyncMode, error) {
	if s.acceptImpl == nil {
		return block.StateSyncSkipped, fmt.Errorf("accept implementation not specified for summary: %s", s)
	}
	return s.acceptImpl(s)
}
