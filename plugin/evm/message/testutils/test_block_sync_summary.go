// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

var BlockSyncSummaryCodec codec.Manager

func init() {
	var err error
	BlockSyncSummaryCodec, err = message.NewCodec(message.BlockSyncSummary{})
	if err != nil {
		panic(err)
	}
}
