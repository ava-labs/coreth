// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/ava-labs/coreth/plugin/evm/extension"
)

type State uint8

const (
	NotStarted State = iota + 1
	InProgress
	Finalizing
	Executing
	Completed
)

type StateMachine struct {
	currentState atomic.Value
}

func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

func (sm *StateMachine) AddTransition(ctx context.Context, block extension.ExtendedBlock, st extension.StateTransition) (bool, error) {
	switch sm.currentState.Load() {
	case NotStarted:
		return false, errors.New("shouldn't be called yet")
	case InProgress:
		// UpdateSyncTarget and flush and tracking recent blocks
	case Finalizing:
		// Add to queue
	case Executing:
		// Add to queue (more carefully)
	case Completed:
		// No-op
		return false, nil
	}
	return false, errors.New("unknown state")
}
