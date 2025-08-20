// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"context"
	"sync"
	"testing"
	"time"
)

func NewTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	if d, ok := t.Deadline(); ok {
		return context.WithDeadline(context.Background(), d)
	}
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func WaitGroupWithContext(t *testing.T, ctx context.Context, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		// include context error for easier debugging
		t.Fatalf("timeout waiting for response: %v", ctx.Err())
	case <-done:
	}
}

// WaitGroupWithTimeout waits for wg with timeout, failing the test with msg on timeout.
func WaitGroupWithTimeout(t *testing.T, wg *sync.WaitGroup, timeout time.Duration, msg string) {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
		return
	case <-time.After(timeout):
		t.Fatal(msg)
	}
}

// WaitErrWithTimeout waits to receive an error from ch or fails on timeout.
func WaitErrWithTimeout(t *testing.T, ch <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		t.Fatal("timed out waiting for RunSyncerTasks to complete")
		return nil
	}
}

// WaitSignalWithTimeout waits for a signal from ch or fails on timeout with msg.
func WaitSignalWithTimeout(t *testing.T, ch <-chan struct{}, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(timeout):
		t.Fatal(msg)
	}
}

func SleepWithContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		return
	}
}
