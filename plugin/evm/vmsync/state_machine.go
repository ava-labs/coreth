// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/coreth/plugin/evm/extension"
	syncpkg "github.com/ava-labs/coreth/sync"
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

	started  chan struct{}
	finished chan struct{}
	onFinish func() error

	queue    *BlockQueue
	registry *SyncerRegistry
}

func NewSyncTracker(started, finished chan struct{}, onFinish func() error) *SyncTracker {
	sm := &SyncTracker{
		currentState: NotStarted,
		queue:        &BlockQueue{},
		registry:     NewSyncerRegistry(),
		started:      started,
		finished:     finished,
		onFinish:     onFinish,
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
		if ok := sm.queue.Enqueue(block, st); !ok {
			return false, errors.New("unable to enqueue block")
		}
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
	// Start syncers
	if err := sm.registry.StartSyncerTasks(ctx, sm.target); err != nil {
		sm.SetState(Completed)
		return err
	}

	sm.SetState(Syncing)
	close(sm.started)
	if err := sm.registry.WaitSyncerTasks(ctx); err != nil {
		sm.SetState(Completed)
		return err
	}

	sm.SetState(Finalizing)

	// Finalize syncers
	if err := sm.registry.FinalizeSync(ctx, sm.target); err != nil {
		sm.SetState(Completed)
		return err
	}
	sm.SetState(Executing)

	// Execute queued blocks
	if err := sm.onFinish(); err != nil {
		sm.SetState(Completed)
		return err
	}

	for {
		done, err := sm.queue.PopAndExecute()
		if err != nil {
			sm.SetState(Completed)
			return err
		}
		if done {
			break
		}
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
	lock  sync.Mutex
	queue []QueueElement
	done  bool
}

type QueueElement struct {
	block extension.ExtendedBlock
	st    extension.StateTransition
}

// Returns true if it has never flushed.
func (bq *BlockQueue) Enqueue(block extension.ExtendedBlock, st extension.StateTransition) bool {
	bq.lock.Lock()
	defer bq.lock.Unlock()
	if bq.done {
		return false
	}
	bq.queue = append(bq.queue, QueueElement{block: block, st: st})
	return true
}

func (bq *BlockQueue) Drop() {
	bq.lock.Lock()
	defer bq.lock.Unlock()
	bq.queue = nil
}

func (bq *BlockQueue) PopAndExecute() (bool, error) {
	bq.lock.Lock()
	defer bq.lock.Unlock()
	if len(bq.queue) == 0 {
		bq.done = true
		return true, nil
	}
	elem := bq.queue[0]
	bq.queue = bq.queue[1:]
	// TODO: actually execute the block
	return false, nil
}
