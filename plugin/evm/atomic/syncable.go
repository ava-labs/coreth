// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ message.Syncable = &AtomicBlockSyncSummary{}

// AtomicBlockSyncSummary provides the information necessary to sync a node starting
// at the given block.
type AtomicBlockSyncSummary struct {
	BlockNumber uint64      `serialize:"true"`
	BlockHash   common.Hash `serialize:"true"`
	BlockRoot   common.Hash `serialize:"true"`
	AtomicRoot  common.Hash `serialize:"true"`

	summaryID  ids.ID
	bytes      []byte
	acceptImpl message.AcceptImplFn
}

func init() {
	message.SyncSummaryType = &AtomicBlockSyncSummary{}
}

type atomicSyncSummaryParser struct{}

func NewAtomicSyncSummaryParser() message.SyncableParser {
	return &atomicSyncSummaryParser{}
}

func (b *atomicSyncSummaryParser) ParseFromBytes(summaryBytes []byte, acceptImpl message.AcceptImplFn) (message.Syncable, error) {
	summary := AtomicBlockSyncSummary{}
	if codecVersion, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
		return nil, err
	} else if codecVersion != message.Version {
		return nil, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (%d != %d)", codecVersion, message.Version)
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

func NewAtomicSyncSummary(blockHash common.Hash, blockNumber uint64, blockRoot common.Hash, atomicRoot common.Hash) (message.Syncable, error) {
	summary := AtomicBlockSyncSummary{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		BlockRoot:   blockRoot,
		AtomicRoot:  atomicRoot,
	}
	bytes, err := Codec.Marshal(message.Version, &summary)
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

func (a *AtomicBlockSyncSummary) GetBlockNumber() uint64 {
	return a.BlockNumber
}

func (a *AtomicBlockSyncSummary) GetBlockHash() common.Hash {
	return a.BlockHash
}

func (a *AtomicBlockSyncSummary) GetBlockRoot() common.Hash {
	return a.BlockRoot
}

func (a *AtomicBlockSyncSummary) Bytes() []byte {
	return a.bytes
}

func (a *AtomicBlockSyncSummary) Height() uint64 {
	return a.BlockNumber
}

func (a *AtomicBlockSyncSummary) ID() ids.ID {
	return a.summaryID
}

func (a *AtomicBlockSyncSummary) String() string {
	return fmt.Sprintf("SyncSummary(BlockHash=%s, BlockNumber=%d, BlockRoot=%s, AtomicRoot=%s)", a.BlockHash, a.BlockNumber, a.BlockRoot, a.AtomicRoot)
}

func (a *AtomicBlockSyncSummary) Accept(context.Context) (block.StateSyncMode, error) {
	if a.acceptImpl == nil {
		return block.StateSyncSkipped, fmt.Errorf("accept implementation not specified for summary: %s", a)
	}
	return a.acceptImpl(a)
}
