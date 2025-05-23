// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/crypto"
)

type BlockSyncSummaryParser struct{}

func NewBlockSyncSummaryParser() *BlockSyncSummaryParser {
	return &BlockSyncSummaryParser{}
}

func (b *BlockSyncSummaryParser) ParseFromBytes(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error) {
	summary := BlockSyncSummary{}
	if codecVersion, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
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
