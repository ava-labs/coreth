// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/lock"

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

	buildBlockLock sync.Mutex
	// lastBuildTime is the time when the last block was built.
	// This is used to ensure that we don't build blocks too frequently,
	// but at least after a minimum delay of minBlockBuildingRetryDelay.
	lastBuildTime time.Time
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
func (b *blockBuilder) handleGenerateBlock() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.lastBuildTime = time.Now()
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
func (b *blockBuilder) markBuilding() {
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
				b.markBuilding()
			case <-extraChan:
				log.Trace("New extra Tx detected, trying to generate a block")
				b.markBuilding()
			case <-b.shutdownChan:
				return
			}
		}
	})
}

func (b *blockBuilder) waitForEvent(ctx context.Context) (commonEng.Message, error) {
	b.buildBlockLock.Lock()

	for !b.needToBuild() {
		if err := b.pendingSignal.Wait(ctx); err != nil {
			b.buildBlockLock.Unlock()
			return 0, err
		}
	}

	// We may only build a block minBlockBuildingRetryDelay after the last block time we have built a block.
	var remainingTimeUntilNextBuild time.Duration

	timeSinceLastBuildTime := time.Since(b.lastBuildTime)
	if b.lastBuildTime.IsZero() || timeSinceLastBuildTime >= minBlockBuildingRetryDelay {
		b.ctx.Log.Debug("Last time we built a block was too long ago, no need to wait",
			zap.Duration("time_since_last_block_build", timeSinceLastBuildTime))
	} else {
		remainingTimeUntilNextBuild = minBlockBuildingRetryDelay - timeSinceLastBuildTime
		b.ctx.Log.Debug("Last time we built a block was not long ago, waiting",
			zap.Duration("time_to_wait_until_next_block_build", remainingTimeUntilNextBuild))
	}

	// We unlock to prevent 'markBuilding' from waiting.
	b.buildBlockLock.Unlock()

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(remainingTimeUntilNextBuild):
		return commonEng.PendingTxs, nil
	}
}
