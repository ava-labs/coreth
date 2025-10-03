// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customheader"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

const (
	// Minimum amount of time to wait after building a block before attempting to build a block
	// a second time without changing the contents of the mempool.
	PreGraniteMinBlockBuildingRetryDelay = 500 * time.Millisecond
	// Minimum amount of time to wait after attempting/build a block before attempting to build another block
	// This is only applied for retrying to build a block after a minimum delay has passed.
	// The initial minimum delay is applied according to parent minDelayExcess (if available)
	// TODO (ceyonur): Decide whether this a correct value.
	PostGraniteMinBlockBuildingRetryDelay = 100 * time.Millisecond
)

type blockBuilder struct {
	clock       *mockable.Clock
	ctx         *snow.Context
	chainConfig *extras.ChainConfig

	txPool       *txpool.TxPool
	extraMempool extension.BuilderMempool

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	pendingSignal *lock.Cond

	buildBlockLock sync.Mutex
	// lastBuildParentHash is the parent hash of the last block that was built.
	// This and lastBuildTime are used to ensure that we don't build blocks too frequently,
	// but at least after a minimum delay of minBlockBuildingRetryDelay.
	lastBuildParentHash common.Hash
	lastBuildTime       time.Time
}

// NewBlockBuilder creates a new block builder. extraMempool is an optional mempool (can be nil) that
// can be used to add transactions to the block builder, in addition to the txPool.
func (vm *VM) NewBlockBuilder(extraMempool extension.BuilderMempool) *blockBuilder {
	b := &blockBuilder{
		ctx:          vm.ctx,
		chainConfig:  vm.chainConfigExtra(),
		txPool:       vm.txPool,
		extraMempool: extraMempool,
		shutdownChan: vm.shutdownChan,
		shutdownWg:   &vm.shutdownWg,
		clock:        vm.clock,
	}
	b.pendingSignal = lock.NewCond(&b.buildBlockLock)
	return b
}

// handleGenerateBlock is called from the VM immediately after BuildBlock.
func (b *blockBuilder) handleGenerateBlock(currentParentHash common.Hash) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.lastBuildTime = b.clock.Time()
	b.lastBuildParentHash = currentParentHash
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
func (b *blockBuilder) waitForEvent(ctx context.Context, currentHeader *types.Header) (commonEng.Message, error) {
	lastBuildTime, lastBuildParentHash, err := b.waitForNeedToBuild(ctx)
	if err != nil {
		return 0, err
	}
	timeUntilNextBuild, shouldBuildImmediately, err := b.calculateBlockBuildingDelay(
		lastBuildTime,
		lastBuildParentHash,
		currentHeader,
	)
	if err != nil {
		return 0, err
	}
	if shouldBuildImmediately {
		b.ctx.Log.Debug("Last time we built a block was long enough ago or this is not a retry, no need to wait")
		return commonEng.PendingTxs, nil
	}

	b.ctx.Log.Debug("Last time we built a block was too recent, waiting",
		zap.Duration("timeUntilNextBuild", timeUntilNextBuild),
	)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(timeUntilNextBuild):
		return commonEng.PendingTxs, nil
	}
}

// waitForNeedToBuild waits until needToBuild returns true.
// It returns the last time a block was built.
func (b *blockBuilder) waitForNeedToBuild(ctx context.Context) (time.Time, common.Hash, error) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	for !b.needToBuild() {
		if err := b.pendingSignal.Wait(ctx); err != nil {
			return time.Time{}, common.Hash{}, err
		}
	}
	return b.lastBuildTime, b.lastBuildParentHash, nil
}

// getMinBlockBuildingDelays returns the initial min block building delay and the minimum retry delay.
// It implements the following logic:
// 1. If the current header is in Granite, return the remaining ACP-226 delay after the parent block time and the minimum retry delay.
// 2. If the current header is not in Granite, return 0 and the minimum retry delay.
func (b *blockBuilder) getMinBlockBuildingDelays(currentHeader *types.Header, config *extras.ChainConfig) (time.Duration, time.Duration, error) {
	// TODO Cleanup (ceyonur): this check can be removed after Granite is activated.
	currentTimestamp := b.clock.Unix()
	if !config.IsGranite(currentTimestamp) {
		return 0, PreGraniteMinBlockBuildingRetryDelay, nil // Pre-Granite: no initial delay
	}

	acp226DelayExcess, err := customheader.MinDelayExcess(config, currentHeader, currentTimestamp, nil)
	if err != nil {
		return 0, 0, err
	}
	acp226Delay := time.Duration(acp226DelayExcess.Delay()) * time.Millisecond

	// Calculate initial delay: time since parent minus ACP-226 delay (clamped to 0)
	parentBlockTime := customtypes.BlockTime(currentHeader)
	timeSinceParentBlock := b.clock.Time().Sub(parentBlockTime)
	// TODO question (ceyonur): should we just use acp226Delay if timeSinceParentBlock is negative?
	initialMinBlockBuildingDelay := acp226Delay - timeSinceParentBlock
	if initialMinBlockBuildingDelay < 0 {
		initialMinBlockBuildingDelay = 0
	}

	return initialMinBlockBuildingDelay, PostGraniteMinBlockBuildingRetryDelay, nil
}

// calculateBlockBuildingDelay calculates the delay needed before building the next block.
// It returns the time to wait, a boolean indicating whether to build immediately, and any error.
// It implements the following logic:
// 1. If there is no initial min block building delay
// 2. if this is not a retry
// 3. if the time since the last build is greater than the minimum retry delay
// then we can build a block immediately.
func (b *blockBuilder) calculateBlockBuildingDelay(
	lastBuildTime time.Time,
	lastBuildParentHash common.Hash,
	currentHeader *types.Header,
) (time.Duration, bool, error) {
	initialMinBlockBuildingDelay, minBlockBuildingRetryDelay, err := b.getMinBlockBuildingDelays(currentHeader, b.chainConfig)
	if err != nil {
		return 0, false, err
	}

	isRetry := lastBuildParentHash == currentHeader.ParentHash && !lastBuildTime.IsZero() // if last build time is zero, this is not a retry

	timeSinceLastBuildTime := b.clock.Time().Sub(lastBuildTime)
	var remainingMinDelay time.Duration
	if minBlockBuildingRetryDelay > timeSinceLastBuildTime {
		remainingMinDelay = minBlockBuildingRetryDelay - timeSinceLastBuildTime
	}

	if initialMinBlockBuildingDelay > 0 {
		remainingMinDelay = max(initialMinBlockBuildingDelay, remainingMinDelay)
	} else if !isRetry || remainingMinDelay == 0 {
		return 0, true, nil // Build immediately
	}

	return remainingMinDelay, false, nil // Need to wait
}
