// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/plugin/evm/message"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
)

var _ message.Syncable = (*AtomicSyncSummary)(nil)

// Codec is the codec manager that contains the codec for AtomicSyncSummary and
// other message types that are used in the network protocol. This is to ensure that the codec
// version is consistent across all message types and includes the codec for AtomicSyncSummary.
var Codec codec.Manager

func init() {
	var err error
	Codec, err = message.NewCodec(AtomicSyncSummary{})
	if err != nil {
		panic(fmt.Errorf("failed to create codec manager: %w", err))
	}
}

// AtomicSyncSummary provides the information necessary to sync a node starting
// at the given block.
type AtomicSyncSummary struct {
	*message.BlockSyncSummary `serialize:"true"`
	AtomicRoot                common.Hash `serialize:"true"`

	summaryID  ids.ID
	bytes      []byte
	acceptImpl message.AcceptImplFn
}

func NewAtomicSyncSummary(blockHash common.Hash, blockNumber uint64, blockRoot common.Hash, atomicRoot common.Hash) (*AtomicSyncSummary, error) {
	summary := AtomicSyncSummary{
		BlockSyncSummary: &message.BlockSyncSummary{
			BlockNumber: blockNumber,
			BlockHash:   blockHash,
			BlockRoot:   blockRoot,
		},
		AtomicRoot: atomicRoot,
	}
	bytes, err := Codec.Marshal(message.Version, &summary)
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

func (a *AtomicSyncSummary) GetBlockHash() common.Hash {
	return a.BlockHash
}

func (a *AtomicSyncSummary) GetBlockRoot() common.Hash {
	return a.BlockRoot
}

func (a *AtomicSyncSummary) Bytes() []byte {
	return a.bytes
}

func (a *AtomicSyncSummary) Height() uint64 {
	return a.BlockNumber
}

func (a *AtomicSyncSummary) ID() ids.ID {
	return a.summaryID
}

func (a *AtomicSyncSummary) String() string {
	return fmt.Sprintf("AtomicSyncSummary(BlockHash=%s, BlockNumber=%d, BlockRoot=%s, AtomicRoot=%s)", a.BlockHash, a.BlockNumber, a.BlockRoot, a.AtomicRoot)
}

func (a *AtomicSyncSummary) Accept(context.Context) (block.StateSyncMode, error) {
	if a.acceptImpl == nil {
		return block.StateSyncSkipped, fmt.Errorf("accept implementation not specified for summary: %s", a)
	}
	return a.acceptImpl(a)
}
