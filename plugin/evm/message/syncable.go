// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ Syncable = (*BlockSyncSummary)(nil)

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
