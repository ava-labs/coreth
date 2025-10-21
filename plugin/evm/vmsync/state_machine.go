// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/coreth/plugin/evm/extension"
	syncpkg "github.com/ava-labs/coreth/sync"
	"github.com/ava-labs/libevm/log"
)

type State uint8

const (
	NotStarted State = iota + 1
	Syncing
	Finalizing
	Executing
	Completed

	PIVOT = 1000
)

type SyncTracker struct {
	lock         sync.Mutex
	currentState State
	target       extension.ExtendedBlock

	queue    *BlockQueue
	registry *SyncerRegistry
}

func NewSyncTracker() *SyncTracker {
	sm := &SyncTracker{
		currentState: NotStarted,
		queue:        &BlockQueue{},
		registry:     NewSyncerRegistry(),
	}
	return sm
}

func (sm *SyncTracker) RegisterSyncer(syncer syncpkg.Syncer) error {
	return sm.registry.Register(syncer)
}

// Bool represents whether the block was processed (true) or deferred to the caller (false).
// On error, bool is meaningless.
func (sm *SyncTracker) Run(ctx context.Context, block extension.ExtendedBlock, st extension.StateTransition) (bool, error) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	switch sm.currentState {
	case NotStarted:
		return false, errors.New("shouldn't be called yet")
	case Syncing:
		// UpdateSyncTarget and flush and tracking recent blocks
		if block.Height()%PIVOT == 0 && st == extension.Accept {
			sm.queue.Drop()
			if err := sm.registry.UpdateSyncTarget(ctx, block); err != nil {
				return false, err
			}
			return true, nil
		}
		sm.queue.Enqueue(block, st)
		return true, nil
	case Finalizing, Executing:
		// Add to queue
		return !sm.queue.Enqueue(block, st), nil
	case Completed:
		// No-op
		return false, nil
	}
	return false, errors.New("unknown state")
}

func (sm *SyncTracker) Sync(ctx context.Context) error {
	// TODO: ensure the state transition is valid
	// Start syncers
	if err := sm.registry.StartSyncerTasks(ctx, sm.target); err != nil {
		sm.SetState(Completed)
		return err
	}

	sm.SetState(Syncing)
	// tell avago we started
	if err := sm.registry.WaitSyncerTasks(ctx); err != nil {
		sm.SetState(Completed)
		return err
	}

	sm.SetState(Finalizing)

	// Finalize syncers
	if err := sm.registry.FinalizeSync(ctx); err != nil {
		sm.SetState(Completed)
		return err
	}
	sm.SetState(Executing)

	// `finishSync`
	// Execute queued blocks
	for {
		block, st, ok := sm.queue.Dequeue()
		if !ok {
			break
		}
		// Apply block to engine
		log.Debug("applying queued block", "height", block.Height(), "st", st)
	}
	sm.SetState(Completed)
	// Notify engine that state sync is complete
	return nil
}

func (sm *SyncTracker) SetState(state State) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	sm.currentState = state
}

type BlockQueue struct {
	queue []QueueElement
}

type QueueElement struct {
	block extension.ExtendedBlock
	st    extension.StateTransition
}

// Returns true if it has never flushed.
// TODO: actually do that
func (bq *BlockQueue) Enqueue(block extension.ExtendedBlock, st extension.StateTransition) bool {
	bq.queue = append(bq.queue, QueueElement{block: block, st: st})
	return true
}

func (bq *BlockQueue) Drop() {
	bq.queue = nil
}

func (bq *BlockQueue) Dequeue() (extension.ExtendedBlock, extension.StateTransition, bool) {
	if len(bq.queue) == 0 {
		return nil, 0, false
	}
	elem := bq.queue[0]
	bq.queue = bq.queue[1:]
	return elem.block, elem.st, true
}
