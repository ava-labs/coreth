// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/sdk/snow"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// Input, Output, Accepted

type InputBlock struct {
	*types.Block
}

func (i *InputBlock) String() string {
	return fmt.Sprintf("EVM block, ID = %s", i.Block.Hash())
}

func (i *InputBlock) GetID() ids.ID {
	return ids.ID(i.Block.Hash())
}

func (i *InputBlock) GetParent() ids.ID {
	return ids.ID(i.Block.ParentHash())
}

func (i *InputBlock) GetTimestamp() int64 {
	return int64(i.Block.Time())
}

func (i *InputBlock) GetBytes() []byte {
	b, err := rlp.EncodeToBytes(i.Block)
	if err != nil {
		panic(err)
	}
	return b
}

func (i *InputBlock) GetHeight() uint64 {
	return i.Block.NumberU64()
}

func (i *InputBlock) GetContext() *block.Context {
	return nil // TODO
}

// GetContext returns the P-Chain context of the block.
// May return nil if there is no P-Chain context, which
// should only occur prior to ProposerVM activation.
// This will be verified from the snow package, so that the
// inner chain can simply use its embedded context.
// GetContext() *block.Context

type OutputBlock struct {
	*InputBlock

	PredicateContext *precompileconfig.PredicateContext
	AtomicState      AtomicState
}

type ConcreteVM struct {
	VM *VM
}

func (vm *ConcreteVM) Initialize(
	ctx context.Context,
	chainInput snow.ChainInput,
	snowApp *snow.VM[*InputBlock, *OutputBlock, *OutputBlock],
) (snow.ChainIndex[*InputBlock], *OutputBlock, *OutputBlock, bool, error) {
	configBytes, err := json.Marshal(chainInput.Config)
	if err != nil {
		return nil, nil, nil, false, err
	}
	if err := vm.VM.Initialize(
		ctx,
		chainInput.SnowCtx,
		nil, /* db: need to feed this in from AvalancheGo */
		chainInput.GenesisBytes,
		chainInput.UpgradeBytes,
		configBytes,
		chainInput.ToEngine,
		nil, /* no fxs */
		nil, /* no appSender: need to feed this in from AvalancheGo for peer/network.go */
	); err != nil {
		return nil, nil, nil, false, err
	}

	// TODO:
	// implement and serve chainIndex from existing code
	// return last accepted block from VM (how does this work with existing vm initialize?)
	// is the state available? can I fetch it from state sync client?
	// Implmenet Build/Verify/Accept/Reject
	// migrate APIs
	// set Connector / Networking
	// set HealthChecker
	// subscribe for SetState handling
	// migrate Shutdown logic
	// migrate version
	// migrate StateSync + handle dynamic state sync
	// remove chain state
	// add SetPreference callbacks because we depend on these notifications to trigger re-orgs early

	// what does this get us?
	// remove chain state
	// break services out of conglomerate type and make them clearly define the events they depend on
	// handle dynamic state sync via the VM SDK instead of re-implementing from scratch

	// cons:
	// still maintains a large surface area between components that must be woven together in Initialize "shallow abstractions"
	// need to modify SDK to support static state sync as well

	return nil, nil, nil, false, nil
}

// Initialize

func (vm *ConcreteVM) ParseBlock(ctx context.Context, bytes []byte) (*InputBlock, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(bytes, ethBlock); err != nil {
		return nil, err
	}
	return &InputBlock{Block: ethBlock}, nil
}
