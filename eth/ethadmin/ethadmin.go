// Copyright (C) 2022, Chain4Travel AG. All rights reserved.

package ethadmin

import (
	"bytes"
	"context"
	"math/big"
	"sync"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

var (
	contractAddr = common.Address{
		1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 10,
	}
	baseFeeSlot = common.Hash{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
	}
)

type AdminControllerBackend interface {
	StateByHeader(ctx context.Context, header *types.Header) (*state.StateDB, error)
	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
}

type AdminController struct {
	ctx              context.Context
	backend          AdminControllerBackend
	lastHead         common.Hash // Last block hash used for validating sequence
	scanHeight       big.Int     // Last scanned block height
	lastChangeHeight big.Int     // Last known block where changes were made

	baseFee *big.Int // BaseFee valid from lastChangeHeight to

	lock sync.RWMutex
}

// NewAdmin returns a new Admin instance used for fast admi state retrieval
func NewController(backend AdminControllerBackend) *AdminController {
	admin := &AdminController{
		ctx:     context.Background(),
		backend: backend,
	}

	headEvent := make(chan core.ChainHeadEvent, 1)
	backend.SubscribeChainHeadEvent(headEvent)
	go func() {
		for ev := range headEvent {
			admin.consume(&ev)
		}
	}()

	return admin
}

func (a *AdminController) consume(ev *core.ChainHeadEvent) {
	// Acquire write lock
	a.lock.Lock()
	defer a.lock.Unlock()

	changed := true
	if a.lastHead == ev.Block.ParentHash() {
		// We are in order, scan for admin tx
		changed = a.process(ev)
	}
	a.lastHead = ev.Block.Hash()
	blockHeight := ev.Block.Number()

	// If a relevant admin TX found, reset cache
	if changed {
		a.lastChangeHeight = *blockHeight
		a.baseFee = nil
	}
	a.scanHeight = *blockHeight
}

func (a *AdminController) process(ev *core.ChainHeadEvent) bool {
	changed := false
	for _, tx := range ev.Block.Transactions() {
		if tx.To() != nil && bytes.Equal(tx.To().Bytes(), contractAddr[:]) {
			changed = true
		}
	}
	return changed
}

func (a *AdminController) GetFixedBaseFee(head *types.Header, state *state.StateDB) *big.Int {
	a.lock.RLock()
	if a.baseFee != nil && a.inScanRange(head) {
		a.lock.RUnlock()
		return a.baseFee
	}
	a.lock.RUnlock()

	if state == nil {
		var err error
		if state, err = a.backend.StateByHeader(a.ctx, head); err != nil {
			log.Debug("cannot get stateDB -> using default: %s", err)
			return new(big.Int).SetUint64(params.SunrisePhase0BaseFee)
		}
	}

	baseFee := new(big.Int).SetBytes(state.GetState(contractAddr, baseFeeSlot).Bytes())

	if baseFee.Cmp(common.Big0) == 0 {
		baseFee.SetUint64(params.SunrisePhase0BaseFee)
	}

	a.lock.Lock()
	defer a.lock.Unlock()
	if a.baseFee == nil && a.inScanRange(head) {
		a.baseFee = baseFee
	}

	return baseFee
}

func (a *AdminController) inScanRange(head *types.Header) bool {
	return head.Number.Cmp(&a.lastChangeHeight) >= 0 && head.Number.Cmp(&a.scanHeight) <= 0
}
