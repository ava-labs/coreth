// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/blocks"
	"github.com/ava-labs/strevm/adaptor"

	corethdatabase "github.com/ava-labs/coreth/plugin/evm/database"
	sae "github.com/ava-labs/strevm"
)

type vmInterface interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.WithVerifyContext
	block.StateSyncableVM
}

var _ vmInterface = (*TransitionVM)(nil)

type TransitionVM struct {
	AtomicVM                                      // current vm backend
	outstandingAppRequests set.Set[ids.RequestID] // protected by atomicVM lock

	chainCtx     *snow.Context
	db           database.Database
	genesisBytes []byte
	upgradeBytes []byte
	configBytes  []byte
	toEngine     chan<- common.Message
	fxs          []*common.Fx
	appSender    common.AppSender

	preFork *VM

	saeVM    *sae.VM
	postFork vmInterface
}

type saeWrapper struct {
	*sae.VM
}

func (*saeWrapper) Initialize(context.Context, *snow.Context, database.Database, []byte, []byte, []byte, chan<- common.Message, []*common.Fx, common.AppSender) error {
	return errors.New("unexpected call to saeWrapper.Initialize")
}

func (t *TransitionVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	if err := t.preFork.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs, appSender); err != nil {
		return fmt.Errorf("initializing preFork VM: %w", err)
	}
	t.Set(t.preFork)

	t.chainCtx = chainCtx
	t.db = db
	t.genesisBytes = genesisBytes
	t.upgradeBytes = upgradeBytes
	t.configBytes = configBytes
	t.toEngine = toEngine
	t.fxs = fxs
	t.appSender = appSender

	lastAcceptedID, err := t.preFork.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("getting preFork last accepted ID: %w", err)
	}

	lastAccepted, err := t.preFork.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("getting preFork last accepted %q: %w", lastAcceptedID, err)
	}

	if err := t.afterAccept(ctx, lastAccepted); err != nil {
		return fmt.Errorf("running post accept hook on %q: %w", lastAcceptedID, err)
	}
	return nil
}

func (t *TransitionVM) afterAccept(ctx context.Context, block snowman.Block) error {
	lastAcceptedTimestamp := block.Timestamp()
	if !t.chainCtx.NetworkUpgrades.IsGraniteActivated(lastAcceptedTimestamp) {
		return nil
	}

	if err := t.preFork.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down preFork VM: %w", err)
	}

	db := corethdatabase.WrapDatabase(prefixdb.NewNested(ethDBPrefix, t.db))
	_ = db
	vm, err := sae.New(
		ctx,
		sae.Config{},
	)
	if err != nil {
		return fmt.Errorf("initializing postFork VM: %w", err)
	}

	t.postFork = adaptor.Convert[*blocks.Block](&saeWrapper{
		VM: vm,
	})

	t.postFork = vm
	// t.Set(t.postFork)
	return nil
}

func (t *TransitionVM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	request := ids.RequestID{
		NodeID:    nodeID,
		RequestID: requestID,
	}

	t.AtomicVM.lock.Lock()
	shouldHandle := t.outstandingAppRequests.Contains(request)
	t.outstandingAppRequests.Remove(request)

	vm := t.AtomicVM.value
	t.AtomicVM.lock.Unlock()

	if !shouldHandle {
		return nil
	}
	return vm.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (t *TransitionVM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	request := ids.RequestID{
		NodeID:    nodeID,
		RequestID: requestID,
	}

	t.AtomicVM.lock.Lock()
	shouldHandle := t.outstandingAppRequests.Contains(request)
	t.outstandingAppRequests.Remove(request)

	vm := t.AtomicVM.value
	t.AtomicVM.lock.Unlock()

	if !shouldHandle {
		return nil
	}
	return vm.AppResponse(ctx, nodeID, requestID, response)
}
