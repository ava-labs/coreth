// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/libevm/crypto"
)

var _ message.SyncableParser = (*atomicSyncSummaryParser)(nil)

type atomicSyncSummaryParser struct{}

func NewAtomicSyncSummaryParser() *atomicSyncSummaryParser {
	return &atomicSyncSummaryParser{}
}

func (a *atomicSyncSummaryParser) ParseFromBytes(summaryBytes []byte, acceptImpl message.AcceptImplFn) (message.Syncable, error) {
	summary := AtomicSyncSummary{}
	if codecVersion, err := codecWithAtomicSync.Unmarshal(summaryBytes, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse syncable summary: %w", err)
	} else if codecVersion != message.Version {
		return nil, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (got %d, expected %d)", codecVersion, message.Version)
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
