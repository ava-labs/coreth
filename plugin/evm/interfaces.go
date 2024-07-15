// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/atx"
	"github.com/ethereum/go-ethereum/common"
)

type atxChain struct {
	*core.BlockChain
}

func (a *atxChain) State() (atx.StateDB, error) {
	state, err := a.BlockChain.State()
	return state, err
}

func (a *atxChain) StateAt(root common.Hash) (atx.StateDB, error) {
	state, err := a.BlockChain.StateAt(root)
	return state, err
}
