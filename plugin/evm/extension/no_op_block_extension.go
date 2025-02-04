package extension

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

var _ BlockManagerExtension = (*noOpBlockExtension)(nil)

type noOpBlockExtension struct{}

func NewNoOpBlockExtension() *noOpBlockExtension {
	return &noOpBlockExtension{}
}

func (noOpBlockExtension) InitializeExtraData(ethBlock *types.Block, chainConfig *params.ChainConfig) (interface{}, error) {
	return nil, nil
}

func (noOpBlockExtension) SyntacticVerify(b VMBlock, rules params.Rules) error {
	return nil
}

func (noOpBlockExtension) Accept(b VMBlock, acceptedBatch database.Batch) error {
	return nil
}

func (noOpBlockExtension) Reject(b VMBlock) error {
	return nil
}

func (noOpBlockExtension) Cleanup(b VMBlock) {}
