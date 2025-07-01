// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/lock"

	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/libevm/log"
)

const (
	// Minimum amount of time to wait after building a block before attempting to build a block
	// a second time without changing the contents of the mempool.
	minBlockBuildingRetryDelay = 500 * time.Millisecond
)

type blockBuilder struct {
	ctx *snow.Context

	txPool       *txpool.TxPool
	extraMempool extension.BuilderMempool

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	pendingSignal *lock.Cond

	// [buildBlockLock] must be held when accessing [buildSent]
	buildBlockLock sync.Mutex

	// buildBlockTimer is a timer used to delay retrying block building a minimum amount of time
	// with the same contents of the mempool.
	// If the mempool receives a new transaction, the block builder will send a new notification to
	// the engine and cancel the timer.
	buildBlockTimer *timer.Timer
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
	b.handleBlockBuilding()
	return b
}

// handleBlockBuilding dispatches a timer used to delay block building retry attempts when the contents
// of the mempool has not been changed since the last attempt.
func (b *blockBuilder) handleBlockBuilding() {
	b.buildBlockTimer = timer.NewTimer(b.buildBlockTimerCallback)
	go b.ctx.Log.RecoverAndPanic(b.buildBlockTimer.Dispatch)
}

// buildBlockTimerCallback is the timer callback that will send a PendingTxs notification
// to the consensus engine if there are transactions in the mempool.
func (b *blockBuilder) buildBlockTimerCallback() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	// If there are still transactions in the mempool, send another notification to
	// the engine to retry BuildBlock.
	if b.needToBuild() {
		b.markBuilding()
	}
}

// handleGenerateBlock is called from the VM immediately after BuildBlock.
func (b *blockBuilder) handleGenerateBlock() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	// Set a timer to check if calling build block a second time is needed.
	b.buildBlockTimer.SetTimeoutIn(minBlockBuildingRetryDelay)
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (b *blockBuilder) needToBuild() bool {
	size := b.txPool.PendingSize(txpool.PendingFilter{
		MinTip: uint256.MustFromBig(b.txPool.GasTip()),
	})
	return size > 0 || (b.extraMempool != nil && b.extraMempool.PendingLen() > 0)
}

// markBuilding notifies a block is expected to be built.
// markBuilding assumes the [buildBlockLock] is held.
func (b *blockBuilder) markBuilding() {
	b.pendingSignal.Broadcast()
	b.buildBlockTimer.Cancel() // Cancel any future attempt from the timer to send a PendingTxs message
}

// signalTxsReady sends a PendingTxs notification to the consensus engine.
// If BuildBlock has not been called since the last PendingTxs message was sent,
// signalTxsReady will not send a duplicate.
func (b *blockBuilder) signalTxsReady() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	// We take a naive approach here and signal the engine that we should build
	// a block as soon as we receive at least one new transaction.
	//
	// In the future, we may wish to add optimization here to only signal the
	// engine if the sum of the projected tips in the mempool satisfies the
	// required block fee.
	b.markBuilding()
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
				b.signalTxsReady()
			case <-extraChan:
				log.Trace("New extra Tx detected, trying to generate a block")
				b.signalTxsReady()
			case <-b.shutdownChan:
				b.buildBlockTimer.Stop()
				return
			}
		}
	})
}

func (b *blockBuilder) waitForTxEnqueue(ctx context.Context) (commonEng.Message, error) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	for !b.needToBuild() {
		if err := b.pendingSignal.Wait(ctx); err != nil {
			return 0, err
		}
	}
	return commonEng.PendingTxs, nil
}

func (b *blockBuilder) wakeup() {
	b.pendingSignal.Broadcast()
}
