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

// funcSyncer is a lightweight Syncer implementation powered by a function.
type funcSyncer struct {
	fn func(ctx context.Context) error
}

func (f funcSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }

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
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			tt.assertState(t, mockSyncers, tt.expectedError)
		})
	}
}

func TestSyncerRegistry_RunSyncerTasks_Concurrency(t *testing.T) {
	// Test timeouts
	const (
		syncerStartTimeout  = 2 * time.Second
		taskCompleteTimeout = 3 * time.Second
		overallTestTimeout  = 5 * time.Second
	)

	// Helper to create a barrier syncer that waits for release.
	createBarrierSyncer := func(wg *sync.WaitGroup, releaseCh <-chan struct{}) funcSyncer {
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

	// Helper to create an error syncer that fails when triggered.
	createErrorSyncer := func(trigger <-chan struct{}, errMsg string) funcSyncer {
		return funcSyncer{fn: func(ctx context.Context) error {
			select {
			case <-trigger:
				return errors.New(errMsg)
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
	}

	// Helper to create a cancellation-aware syncer.
	createCancelAwareSyncer := func(started, canceled chan struct{}) funcSyncer {
		return funcSyncer{fn: func(ctx context.Context) error {
			close(started)
			select {
			case <-ctx.Done():
				close(canceled)
				return ctx.Err()
			case <-time.After(overallTestTimeout):
				return errors.New("syncer timed out waiting for cancellation")
			}
		}}
	}

	// Test helper to reduce duplication and improve readability.
	waitWG := func(wg *sync.WaitGroup, timeout time.Duration, msg string) {
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

	waitErr := func(ch <-chan error, timeout time.Duration) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(timeout):
			t.Fatal("timed out waiting for RunSyncerTasks to complete")
			return nil
		}
	}

	waitSignal := func(ch <-chan struct{}, timeout time.Duration, msg string) {
		t.Helper()
		select {
		case <-ch:
			return
		case <-time.After(timeout):
			t.Fatal(msg)
		}
	}

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

			// Validation checks.
			if tt.numErrorSyncers > 0 && tt.expectedError == "" {
				t.Fatal("test case with error syncers must specify expectedError")
			}

			if tt.numCancelSyncers > 0 && tt.numErrorSyncers == 0 {
				t.Fatal("cancel syncers only make sense with error syncers")
			}

			registry := NewSyncerRegistry()

			ctx, cancel := context.WithTimeout(context.Background(), overallTestTimeout)
			t.Cleanup(cancel)

			var (
				allStartedWG sync.WaitGroup
				releaseCh    chan struct{}
				releaseOnce  sync.Once
				triggers     []chan struct{}
				cancelChans  []chan struct{}
			)

			// Setup barrier syncers if needed.
			if tt.numBarrierSyncers > 0 {
				allStartedWG.Add(tt.numBarrierSyncers)
				releaseCh = make(chan struct{})
				t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCh) }) })

				for i := 0; i < tt.numBarrierSyncers; i++ {
					name := fmt.Sprintf("BarrierSyncer-%d", i)
					syncer := createBarrierSyncer(&allStartedWG, releaseCh)
					require.NoError(t, registry.Register(name, syncer))
				}
			}

			// Setup error syncers if needed.
			if tt.numErrorSyncers > 0 {
				for i := 0; i < tt.numErrorSyncers; i++ {
					trigger := make(chan struct{})
					triggers = append(triggers, trigger)

					name := fmt.Sprintf("ErrorSyncer-%d", i)
					syncer := createErrorSyncer(trigger, tt.errorMsg)
					require.NoError(t, registry.Register(name, syncer))
				}

				// Cleanup triggers.
				t.Cleanup(func() {
					for _, trigger := range triggers {
						select {
						case <-trigger:
						default:
							close(trigger)
						}
					}
				})
			}

			// Setup cancel-aware syncers if needed.
			if tt.numCancelSyncers > 0 {
				for i := 0; i < tt.numCancelSyncers; i++ {
					startedCh := make(chan struct{})
					canceledCh := make(chan struct{})
					cancelChans = append(cancelChans, canceledCh)

					name := fmt.Sprintf("CancelSyncer-%d", i)
					syncer := createCancelAwareSyncer(startedCh, canceledCh)
					require.NoError(t, registry.Register(name, syncer))
				}
			}

			// Start the registry.
			doneCh := make(chan error, 1)
			go func() { doneCh <- registry.RunSyncerTasks(ctx, &client{}) }()

			// Wait for barrier syncers to start if any.
			if tt.numBarrierSyncers > 0 {
				waitWG(&allStartedWG, syncerStartTimeout, "timed out waiting for barrier syncers to start")
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
			err := waitErr(doneCh, taskCompleteTimeout)
			if shouldSucceed(tt) {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.expectedError != "" {
					require.Contains(t, err.Error(), tt.expectedError)
				}

				// Verify cancellation was propagated to cancel-aware syncers.
				for i, cancelCh := range cancelChans {
					waitSignal(cancelCh, syncerStartTimeout, fmt.Sprintf("cancellation was not propagated to cancel syncer %d", i))
				}
			}
		})
	}
}
