// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"context"
	"errors"
	"sync"
	"time"
)

// FuncSyncer is a lightweight Syncer implementation powered by a function.
type FuncSyncer struct {
	fn func(ctx context.Context) error
}

// Sync executes the wrapped function.
func (f FuncSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }

// NewBarrierSyncer returns a syncer that signals start and then blocks until releaseCh closes.
func NewBarrierSyncer(wg *sync.WaitGroup, releaseCh <-chan struct{}) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		wg.Done()
		select {
		case <-releaseCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewErrorSyncer returns a syncer that returns errToReturn when trigger is closed.
func NewErrorSyncer(trigger <-chan struct{}, errToReturn error) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		select {
		case <-trigger:
			return errToReturn
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewCancelAwareSyncer closes started, then waits for ctx cancellation or times out.
func NewCancelAwareSyncer(started, canceled chan struct{}, timeout time.Duration) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return ctx.Err()
		case <-time.After(timeout):
			return errors.New("syncer timed out waiting for cancellation")
		}
	}}
}
