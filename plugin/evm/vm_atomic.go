// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
)

var _ vmInterface = (*AtomicVM)(nil)

type AtomicVM struct {
	lock  sync.RWMutex
	value vmInterface
}

func (a *AtomicVM) Get() vmInterface {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.value
}

func (a *AtomicVM) Set(value vmInterface) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.value = value
}

func (a *AtomicVM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return a.Get().AppGossip(ctx, nodeID, msg)
}

func (a *AtomicVM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return a.Get().AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (a *AtomicVM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	return a.Get().AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (a *AtomicVM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return a.Get().AppResponse(ctx, nodeID, requestID, response)
}

func (a *AtomicVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return a.Get().BuildBlock(ctx)
}

func (a *AtomicVM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	return a.Get().BuildBlockWithContext(ctx, blockCtx)
}

func (a *AtomicVM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return a.Get().Connected(ctx, nodeID, nodeVersion)
}

func (a *AtomicVM) CreateHTTP2Handler(ctx context.Context) (http.Handler, error) {
	return a.Get().CreateHTTP2Handler(ctx)
}

func (a *AtomicVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return a.Get().CreateHandlers(ctx)
}

func (a *AtomicVM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return a.Get().Disconnected(ctx, nodeID)
}

func (a *AtomicVM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	return a.Get().GetBlock(ctx, blkID)
}

func (a *AtomicVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return a.Get().GetBlockIDAtHeight(ctx, height)
}

// func (a *AtomicVM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
// 	return a.Get().GetLastStateSummary(ctx)
// }

// func (a *AtomicVM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
// 	return a.Get().GetOngoingSyncStateSummary(ctx)
// }

// func (a *AtomicVM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
// 	return a.Get().GetStateSummary(ctx, summaryHeight)
// }

func (a *AtomicVM) HealthCheck(ctx context.Context) (interface{}, error) {
	return a.Get().HealthCheck(ctx)
}

func (a *AtomicVM) Initialize(ctx context.Context, chainCtx *snow.Context, db database.Database, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, toEngine chan<- common.Message, fxs []*common.Fx, appSender common.AppSender) error {
	return a.Get().Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs, appSender)
}

func (a *AtomicVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return a.Get().LastAccepted(ctx)
}

func (a *AtomicVM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	return a.Get().ParseBlock(ctx, blockBytes)
}

// func (a *AtomicVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
// 	return a.Get().ParseStateSummary(ctx, summaryBytes)
// }

func (a *AtomicVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return a.Get().SetPreference(ctx, blkID)
}

func (a *AtomicVM) SetState(ctx context.Context, state snow.State) error {
	return a.Get().SetState(ctx, state)
}

func (a *AtomicVM) Shutdown(ctx context.Context) error {
	return a.Get().Shutdown(ctx)
}

// func (a *AtomicVM) StateSyncEnabled(ctx context.Context) (bool, error) {
// 	return a.Get().StateSyncEnabled(ctx)
// }

func (a *AtomicVM) Version(ctx context.Context) (string, error) {
	return a.Get().Version(ctx)
}
