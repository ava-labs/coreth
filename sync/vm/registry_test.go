// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/coreth/utils/utilstest"
	"github.com/stretchr/testify/require"
)

// mockSyncer implements synccommon.Syncer for testing.
type mockSyncer struct {
	name      string
	syncError error
	started   bool // Track if already started
}

func newMockSyncer(name string, syncError error) *mockSyncer {
	return &mockSyncer{
		name:      name,
		syncError: syncError,
	}
}

func (m *mockSyncer) Sync(ctx context.Context) error {
	m.started = true
	return m.syncError
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
		registrations []struct {
			name   string
			syncer *mockSyncer
		}
		expectedError string
		expectedCount int
	}{
		{
			name: "successful registrations",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", newMockSyncer("TestSyncer1", nil)},
				{"Syncer2", newMockSyncer("TestSyncer2", nil)},
			},
			expectedError: "",
			expectedCount: 2,
		},
		{
			name: "duplicate name registration",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", newMockSyncer("Syncer1", nil)},
				{"Syncer1", newMockSyncer("Syncer1", nil)},
			},
			expectedError: "syncer with name 'Syncer1' is already registered",
			expectedCount: 1,
		},
		{
			name: "preserve registration order",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", newMockSyncer("Syncer1", nil)},
				{"Syncer2", newMockSyncer("Syncer2", nil)},
				{"Syncer3", newMockSyncer("Syncer3", nil)},
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
				err := registry.Register(reg.name, reg.syncer)
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
					require.Equal(t, reg.syncer, registry.syncers[i].syncer)
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
		assertState   func(t *testing.T, mockSyncers []*mockSyncer, expectedError string)
	}{
		{
			name: "successful execution",
			syncers: []syncerConfig{
				{"Syncer1", nil},
				{"Syncer2", nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, expectedError string) {
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
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, expectedError string) {
				// First syncer should have been started.
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
			for i, cfg := range tt.syncers {
				mockSyncer := newMockSyncer(cfg.name, cfg.syncError)
				mockSyncers[i] = mockSyncer
				require.NoError(t, registry.Register(cfg.name, mockSyncer))
			}

			err := registry.RunSyncerTasks(context.Background(), &client{})

			if tt.expectedError != "" {
				// Verify the wrapped cause when available.
				if len(tt.syncers) > 0 && tt.syncers[0].syncError != nil {
					require.ErrorIs(t, err, tt.syncers[0].syncError)
				}
			} else {
				require.NoError(t, err)
			}

			tt.assertState(t, mockSyncers, tt.expectedError)
		})
	}
}

func TestSyncerRegistry_RunSyncerTasks_Concurrency(t *testing.T) {
	// Barrier pattern:
	// 1. All barrier syncers begin Sync concurrently (via errgroup).
	// 2. Each syncer signals readiness using allStartedWG.Done().
	// 3. Each syncer blocks until `releaseCh` is closed.
	// 4. The test waits on allStartedWG.Wait() to ensure all have started.
	// 5. The test then closes `releaseCh` to release them simultaneously.
	//
	// This demonstrates true concurrency: if syncers ran sequentially, the test
	// would hang because no syncer could complete to allow the next to begin.

	// Test timeouts
	const (
		syncerStartTimeout  = 2 * time.Second
		taskCompleteTimeout = 3 * time.Second
		overallTestTimeout  = 5 * time.Second
	)

	type testCase struct {
		name              string
		numBarrierSyncers int
		numErrorSyncers   int
		numCancelSyncers  int
		errorMsg          string
		expectedError     string
	}

	// Helper to determine if test should succeed.
	shouldSucceed := func(tc testCase) bool {
		return tc.numErrorSyncers == 0
	}

	tests := []testCase{
		{
			name:              "single syncer succeeds",
			numBarrierSyncers: 1,
		},
		{
			name:              "multiple syncers start concurrently",
			numBarrierSyncers: 5,
		},
		{
			name:              "error syncer cancels barrier syncers",
			numBarrierSyncers: 2,
			numErrorSyncers:   1,
			numCancelSyncers:  1,
			errorMsg:          "test error",
			expectedError:     "ErrorSyncer-0 failed",
		},
		{
			name:             "multiple error syncers - first one wins",
			numErrorSyncers:  3,
			numCancelSyncers: 2,
			errorMsg:         "boom",
			expectedError:    "ErrorSyncer-0 failed",
		},
		{
			name:              "no syncers registered",
			numBarrierSyncers: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := NewSyncerRegistry()

			ctx, cancel := context.WithTimeout(context.Background(), overallTestTimeout)
			t.Cleanup(cancel)

			var (
				allStartedWG sync.WaitGroup
				releaseCh    chan struct{}
				releaseOnce  sync.Once
				triggers     []chan struct{}
				cancelChans  []chan struct{}
				errTargets   []error
			)

			// Setup barrier syncers if needed.
			if tt.numBarrierSyncers > 0 {
				allStartedWG.Add(tt.numBarrierSyncers)
				releaseCh = make(chan struct{})

				for i := 0; i < tt.numBarrierSyncers; i++ {
					name := fmt.Sprintf("BarrierSyncer-%d", i)
					syncer := utilstest.NewBarrierSyncer(&allStartedWG, releaseCh)
					require.NoError(t, registry.Register(name, syncer))
				}
			}

			// Setup error syncers if needed.
			if tt.numErrorSyncers > 0 {
				for i := 0; i < tt.numErrorSyncers; i++ {
					trigger := make(chan struct{})
					triggers = append(triggers, trigger)
					errInstance := errors.New(tt.errorMsg)
					errTargets = append(errTargets, errInstance)

					name := fmt.Sprintf("ErrorSyncer-%d", i)
					syncer := utilstest.NewErrorSyncer(trigger, errInstance)
					require.NoError(t, registry.Register(name, syncer))
				}

			}

			// Setup cancel-aware syncers if needed.
			if tt.numCancelSyncers > 0 {
				for i := 0; i < tt.numCancelSyncers; i++ {
					startedCh := make(chan struct{})
					canceledCh := make(chan struct{})
					cancelChans = append(cancelChans, canceledCh)

					name := fmt.Sprintf("CancelSyncer-%d", i)
					syncer := utilstest.NewCancelAwareSyncer(startedCh, canceledCh, overallTestTimeout)
					require.NoError(t, registry.Register(name, syncer))
				}
			}

			// Start the registry.
			doneCh := make(chan error, 1)
			go func() { doneCh <- registry.RunSyncerTasks(ctx, &client{}) }()

			// Wait for barrier syncers to signal started via allStartedWG.
			if tt.numBarrierSyncers > 0 {
				utilstest.WaitGroupWithTimeout(t, &allStartedWG, syncerStartTimeout, "timed out waiting for barrier syncers to start")
			}

			// Trigger errors if needed.
			if tt.numErrorSyncers > 0 {
				// Close the first trigger to cause an error.
				close(triggers[0])
			}

			// Release barrier syncers if no errors expected.
			if shouldSucceed(tt) && releaseCh != nil {
				releaseOnce.Do(func() { close(releaseCh) })
			}

			// Wait for completion and verify result.
			err := utilstest.WaitErrWithTimeout(t, doneCh, taskCompleteTimeout)
			if shouldSucceed(tt) {
				require.NoError(t, err)
			} else {
				// Assert error type using ErrorIs and skip explicit Contains
				if len(errTargets) > 0 {
					require.ErrorIs(t, err, errTargets[0])
				}

				// Verify cancellation was propagated to cancel-aware syncers.
				for i, cancelCh := range cancelChans {
					utilstest.WaitSignalWithTimeout(t, cancelCh, syncerStartTimeout, fmt.Sprintf("cancellation was not propagated to cancel syncer %d", i))
				}
			}
		})
	}
}
