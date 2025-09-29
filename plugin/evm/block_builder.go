// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/plugin/evm/extension"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

// Minimum amount of time to wait after building a block before attempting to build a block
// a second time without changing the contents of the mempool.
const MinBlockBuildingRetryDelay = 500 * time.Millisecond

type blockBuilder struct {
	ctx *snow.Context

	txPool       *txpool.TxPool
	extraMempool extension.BuilderMempool

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	pendingSignal *lock.Cond

	buildBlockLock sync.Mutex
	// lastBuiltHeight is the height of the last block was built.
	// This is used to ensure that we don't build blocks too frequently,
	// but at least after a minimum delay of minBlockBuildingRetryDelay.
	lastBuiltHeight uint64
	isRetry         bool
}

// NewBlockBuilder creates a new block builder. extraMempool is an optional mempool (can be nil) that
// can be used to add transactions to the block builder, in addition to the txPool.
func (vm *VM) NewBlockBuilder(extraMempool extension.BuilderMempool) *blockBuilder {
	b := &blockBuilder{
		ctx:          vm.ctx,
		txPool:       vm.txPool,
		extraMempool: extraMempool,
		shutdownChan: vm.shutdownChan,
		shutdownWg:   &vm.shutdownWg,
	}
	b.pendingSignal = lock.NewCond(&b.buildBlockLock)
	return b
}

// handleGenerateBlock is called from the VM immediately after BuildBlock.
func (b *blockBuilder) handleGenerateBlock(currentHeight uint64) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.isRetry = b.lastBuiltHeight == currentHeight
	b.lastBuiltHeight = currentHeight
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (b *blockBuilder) needToBuild() bool {
	size := b.txPool.PendingSize(txpool.PendingFilter{
		MinTip: uint256.MustFromBig(b.txPool.GasTip()),
	})
	return size > 0 || (b.extraMempool != nil && b.extraMempool.PendingLen() > 0)
}

// signalCanBuild notifies a block is expected to be built.
func (b *blockBuilder) signalCanBuild() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.pendingSignal.Broadcast()
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (b *blockBuilder) awaitSubmittedTxs() {
	// txSubmitChan is invoked when new transactions are issued as well as on re-orgs which
	// may orphan transactions that were previously in a preferred block.
	txSubmitChan := make(chan core.NewTxsEvent)
	b.txPool.SubscribeTransactions(txSubmitChan, true)

	var extraChan <-chan struct{}
	if b.extraMempool != nil {
		extraChan = b.extraMempool.SubscribePendingTxs()
	}

	b.shutdownWg.Add(1)
	go b.ctx.Log.RecoverAndPanic(func() {
		defer b.shutdownWg.Done()

		for {
			select {
			case <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalCanBuild()
			case <-extraChan:
				log.Trace("New extra Tx detected, trying to generate a block")
				b.signalCanBuild()
			case <-b.shutdownChan:
				return
			}
		}
	})
}

// waitForEvent waits until a block needs to be built.
// It returns only after at least [minBlockBuildingRetryDelay] passed from the last time a block was built.
func (b *blockBuilder) waitForEvent(ctx context.Context) (commonEng.Message, error) {
	isRetry, err := b.waitForNeedToBuild(ctx)
	if err != nil {
		return 0, err
	}
	if !isRetry {
		b.ctx.Log.Debug("This is the first time we attempt to build a block at this height, no need to wait")
		return commonEng.PendingTxs, nil
	}
	b.ctx.Log.Debug("Last time we built a block was too recent, waiting",
		zap.Duration("timeUntilNextBuild", MinBlockBuildingRetryDelay),
	)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(MinBlockBuildingRetryDelay):
		return commonEng.PendingTxs, nil
	}
}

// waitForNeedToBuild waits until needToBuild returns true.
// It returns the last time a block was built.
func (b *blockBuilder) waitForNeedToBuild(ctx context.Context) (bool, error) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	for !b.needToBuild() {
		if err := b.pendingSignal.Wait(ctx); err != nil {
			return false, err
		}
	}
	return b.isRetry, nil
}
