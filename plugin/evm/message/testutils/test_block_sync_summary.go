package testutils

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

var TestBlockSyncSummaryCodec codec.Manager

func init() {
	var err error
	TestBlockSyncSummaryCodec, err = message.NewCodec(message.BlockSyncSummary{})
	if err != nil {
		panic(err)
	}
}
