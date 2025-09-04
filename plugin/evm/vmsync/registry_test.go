// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/utils/utilstest"

	synccommon "github.com/ava-labs/coreth/sync"
)

var (
	_ synccommon.Syncer = (*mockSyncer)(nil)
	_ synccommon.Syncer = (*funcSyncer)(nil)
)

// mockSyncer implements [synccommon.Syncer] for testing.
type mockSyncer struct {
	name      string
	syncError error
	started   bool // Track if already started
}

func newMockSyncer(name string, syncError error) *mockSyncer {
	return &mockSyncer{name: name, syncError: syncError}
}

func (m *mockSyncer) Sync(_ context.Context) error {
	m.started = true
	return m.syncError
}

// funcSyncer adapts a function to the simple Syncer shape used in tests. It is
// useful for defining small, behavior-driven syncers inline.
type funcSyncer struct {
	fn func(ctx context.Context) error
}

// Sync calls the wrapped function and returns its result.
func (f funcSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }

// newBarrierSyncer returns a syncer that, upon entering Sync, calls wg.Done() to
// signal it has started, then blocks until either:
//   - `releaseCh` is closed, returning nil; or
//   - `ctx` is canceled, returning ctx.Err.
//
// This acts as a barrier to coordinate test goroutines.
func newBarrierSyncer(wg *sync.WaitGroup, releaseCh <-chan struct{}) funcSyncer {
	return funcSyncer{fn: func(ctx context.Context) error {
		wg.Done()
		select {
		case <-releaseCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// newErrorSyncer returns a syncer that waits until either `trigger` is closed
// (then returns `errToReturn`) or `ctx` is canceled (then returns ctx.Err).
func newErrorSyncer(trigger <-chan struct{}, errToReturn error) funcSyncer {
	return funcSyncer{fn: func(ctx context.Context) error {
		select {
		case <-trigger:
			return errToReturn
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// newCancelAwareSyncer closes `started` as soon as Sync begins, then waits for
// either:
//   - `ctx` cancellation: closes `canceled` and returns ctx.Err; or
//   - `timeout` elapsing: returns an error indicating a timeout.
//
// Useful for asserting that cancellation propagates to the syncer under test.
func newCancelAwareSyncer(started, canceled chan struct{}, timeout time.Duration) funcSyncer {
	return funcSyncer{fn: func(ctx context.Context) error {
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

// syncerConfig describes a test syncer setup for RunSyncerTasks table tests.
type syncerConfig struct {
	name      string
	syncError error
}

func TestNewSyncerRegistry(t *testing.T) {
	registry := NewSyncerRegistry()
	require.NotNil(t, registry)
	require.Empty(t, registry.syncers)
}

func TestSyncerRegistry_Register(t *testing.T) {
	tests := []struct {
		name          string
		registrations []*mockSyncer
		expectedError string
		expectedCount int
	}{
		{
			name: "successful registrations",
			registrations: []*mockSyncer{
				newMockSyncer("Syncer1", nil),
				newMockSyncer("Syncer2", nil),
			},
			expectedError: "",
			expectedCount: 2,
		},
		{
			name: "duplicate name registration",
			registrations: []*mockSyncer{
				newMockSyncer("Syncer1", nil),
				newMockSyncer("Syncer1", nil),
			},
			expectedError: "syncer with name 'Syncer1' is already registered",
			expectedCount: 1,
		},
		{
			name: "preserve registration order",
			registrations: []*mockSyncer{
				newMockSyncer("Syncer1", nil),
				newMockSyncer("Syncer2", nil),
				newMockSyncer("Syncer3", nil),
			},
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			var errLast error

			// Perform registrations.
			for _, reg := range tt.registrations {
				err := registry.Register(reg.name, reg)
				if err != nil {
					errLast = err
					break
				}
			}

			// Check error expectations.
			if tt.expectedError != "" {
				require.Error(t, errLast)
				require.Contains(t, errLast.Error(), tt.expectedError)
			} else {
				require.NoError(t, errLast)
			}

			// Verify registration count.
			require.Len(t, registry.syncers, tt.expectedCount)

			// Verify registration order for successful cases.
			if tt.expectedError == "" {
				for i, reg := range tt.registrations {
					require.Equal(t, reg.name, registry.syncers[i].name)
					require.Equal(t, reg, registry.syncers[i].syncer)
				}
			}
		})
	}
}

func TestSyncerRegistry_RunSyncerTasks(t *testing.T) {
	tests := []struct {
		name          string
		syncers       []syncerConfig
		expectedError string
		assertState   func(t *testing.T, mockSyncers []*mockSyncer)
	}{
		{
			name: "successful execution",
			syncers: []syncerConfig{
				{"Syncer1", nil},
				{"Syncer2", nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer) {
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.started, "Syncer %d should have been started", i)
				}
			},
		}, {
			name: "error returned",
			syncers: []syncerConfig{
				{"Syncer1", errors.New("wait failed")},
				{"Syncer2", nil},
			},
			expectedError: "Syncer1 failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer) {
				// First syncer should be started and waited on (but wait failed).
				require.True(t, mockSyncers[0].started, "First syncer should have been started")
				// With concurrency, the second may or may not have started -> don't assert it.
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			mockSyncers := make([]*mockSyncer, len(tt.syncers))

			// Register syncers.
			for i, syncerConfig := range tt.syncers {
				mockSyncer := newMockSyncer(syncerConfig.name, syncerConfig.syncError)
				mockSyncers[i] = mockSyncer
				require.NoError(t, registry.Register(syncerConfig.name, mockSyncer))
			}

			ctx, cancel := utilstest.NewTestContext(t)
			t.Cleanup(cancel)

			err := registry.RunSyncerTasks(ctx, getTestClientSummary(t))

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Use custom assertion function for each test case.
			tt.assertState(t, mockSyncers)
		})
	}
}

func TestSyncerRegistry_ConcurrentStart(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := utilstest.NewTestContext(t)
	t.Cleanup(cancel)

	const numBarrierSyncers = 5

	var allStartedWG sync.WaitGroup
	allStartedWG.Add(numBarrierSyncers)

	releaseCh := make(chan struct{})

	for i := 0; i < numBarrierSyncers; i++ {
		name := fmt.Sprintf("BarrierSyncer-%d", i)
		syncer := newBarrierSyncer(&allStartedWG, releaseCh)
		require.NoError(t, registry.Register(name, syncer))
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- registry.RunSyncerTasks(ctx, getTestClientSummary(t)) }()

	utilstest.WaitGroupWithTimeout(t, &allStartedWG, 2*time.Second, "timed out waiting for barrier syncers to start")
	close(releaseCh)

	require.NoError(t, utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second))
}

func TestSyncerRegistry_ErrorPropagatesAndCancelsOthers(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := utilstest.NewTestContext(t)
	t.Cleanup(cancel)

	// Error syncer
	trigger := make(chan struct{})
	errFirst := errors.New("test error")
	require.NoError(t, registry.Register("ErrorSyncer-0", newErrorSyncer(trigger, errFirst)))

	// Cancel-aware syncers to verify cancellation propagation
	const numCancelSyncers = 2
	var cancelChans []chan struct{}
	var startedChans []chan struct{}

	for i := 0; i < numCancelSyncers; i++ {
		startedCh := make(chan struct{})
		canceledCh := make(chan struct{})
		cancelChans = append(cancelChans, canceledCh)
		startedChans = append(startedChans, startedCh)
		name := fmt.Sprintf("CancelSyncer-%d", i)

		require.NoError(t, registry.Register(name, newCancelAwareSyncer(startedCh, canceledCh, 4*time.Second)))
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- registry.RunSyncerTasks(ctx, getTestClientSummary(t)) }()

	// Ensure cancel-aware syncers are running before triggering the error
	for i, started := range startedChans {
		utilstest.WaitSignalWithTimeout(t, started, 2*time.Second, fmt.Sprintf("cancel-aware syncer %d did not start", i))
	}

	close(trigger)

	err := utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second)
	require.ErrorIs(t, err, errFirst)

	for i, cancelCh := range cancelChans {
		utilstest.WaitSignalWithTimeout(t, cancelCh, 2*time.Second, fmt.Sprintf("cancellation was not propagated to cancel syncer %d", i))
	}
}

func TestSyncerRegistry_FirstErrorWinsAcrossMany(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := utilstest.NewTestContext(t)
	t.Cleanup(cancel)

	const numErrorSyncers = 3

	var triggers []chan struct{}
	var errFirst error

	for i := 0; i < numErrorSyncers; i++ {
		trigger := make(chan struct{})
		triggers = append(triggers, trigger)
		errInstance := errors.New("boom")
		if i == 0 {
			errFirst = errInstance
		}
		name := fmt.Sprintf("ErrorSyncer-%d", i)
		require.NoError(t, registry.Register(name, newErrorSyncer(trigger, errInstance)))
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- registry.RunSyncerTasks(ctx, getTestClientSummary(t)) }()

	// Trigger only the first error; others should return due to cancellation
	close(triggers[0])

	err := utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second)
	require.ErrorIs(t, err, errFirst)
}

func TestSyncerRegistry_NoSyncersRegistered(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()
	ctx, cancel := utilstest.NewTestContext(t)
	t.Cleanup(cancel)

	require.NoError(t, registry.RunSyncerTasks(ctx, getTestClientSummary(t)))
}

func getTestClientSummary(t *testing.T) message.Syncable {
	t.Helper()
	summary, err := message.NewBlockSyncSummary(common.HexToHash("0xdeadbeef"), 1000, common.HexToHash("0xdeadbeef"))
	require.NoError(t, err)

	return summary
}
