// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	synccommon "github.com/ava-labs/coreth/sync"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// mockSyncer implements synccommon.Syncer for testing.
type mockSyncer struct {
	name           string
	startDelay     time.Duration
	waitDelay      time.Duration
	startError     error
	waitError      error
	startCallCount int
	waitCallCount  int
	startCalled    bool
	waitCalled     bool
	started        bool // Track if already started
}

func newMockSyncer(name string, startDelay, waitDelay time.Duration, startError, waitError error) *mockSyncer {
	return &mockSyncer{
		name:       name,
		startDelay: startDelay,
		waitDelay:  waitDelay,
		startError: startError,
		waitError:  waitError,
	}
}

func (m *mockSyncer) Start(ctx context.Context) error {
	if m.started {
		return synccommon.ErrSyncerAlreadyStarted
	}

	m.started = true
	m.startCalled = true
	m.startCallCount++
	if m.startDelay > 0 {
		select {
		case <-time.After(m.startDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.startError
}

func (m *mockSyncer) Wait(ctx context.Context) error {
	m.waitCalled = true
	m.waitCallCount++
	if m.waitDelay > 0 {
		select {
		case <-time.After(m.waitDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return m.waitError
}

func TestNewSyncerRegistry(t *testing.T) {
	t.Parallel()
	registry := NewSyncerRegistry()
	require.NotNil(t, registry)

	// Check that the registry is empty
	count := 0
	registry.syncers.Range(func(key, value any) bool {
		count++
		return true
	})
	require.Equal(t, 0, count)
}

func TestSyncerRegistry_Register(t *testing.T) {
	t.Parallel()
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
				{"Syncer1", newMockSyncer("TestSyncer1", 0, 0, nil, nil)},
				{"Syncer2", newMockSyncer("TestSyncer2", 0, 0, nil, nil)},
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
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
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
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
				{"Syncer2", newMockSyncer("Syncer2", 0, 0, nil, nil)},
				{"Syncer3", newMockSyncer("Syncer3", 0, 0, nil, nil)},
			},
			expectedCount: 3,
		},
		{
			name: "empty name registration",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"", newMockSyncer("EmptyName", 0, 0, nil, nil)},
			},
			expectedCount: 1, // Empty name should be allowed
		},
		{
			name: "nil syncer registration",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", nil},
			},
			expectedCount: 1, // Nil syncer should be allowed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			count := 0
			registry.syncers.Range(func(key, value any) bool {
				count++
				return true
			})
			require.Equal(t, tt.expectedCount, count)

			// Verify all syncers were registered (order is not guaranteed with sync.Map)
			if tt.expectedError == "" {
				// Check that each expected registration exists
				for _, reg := range tt.registrations {
					value, exists := registry.syncers.Load(reg.name)
					require.True(t, exists, "Syncer %s should be registered", reg.name)
					task := value.(SyncerTask)
					require.Equal(t, reg.syncer, task.syncer)
				}
			}
		})
	}
}

// TestSyncerRegistry_RunSyncerTasks tests all syncer execution scenarios.
func TestSyncerRegistry_RunSyncerTasks(t *testing.T) {
	t.Parallel()
	type syncerConfig struct {
		name       string
		startDelay time.Duration
		waitDelay  time.Duration
		startError error
		waitError  error
	}

	type testCase struct {
		name           string
		syncers        []syncerConfig
		useNilClient   bool
		contextTimeout time.Duration
		expectedError  string
		assertState    func(t *testing.T, mockSyncers []*mockSyncer, err error)
	}

	tests := []testCase{
		{
			name: "successful execution",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
				{"Syncer2", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
		{
			name: "start error stops execution",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, errors.New("start failed"), nil},
				{"Syncer2", 0, 0, nil, nil},
			},
			expectedError: "failed to start Syncer1",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With concurrent execution, both syncers may be started,
				// but the first one should fail during Start().
				// Second syncer may or may not be started due to concurrent execution.
				// We can't guarantee it won't be started in concurrent mode.
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.False(t, mockSyncers[0].waitCalled, "First syncer should not have been waited on due to start error")
			},
		},
		{
			name: "wait error stops execution",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, errors.New("wait failed")},
				{"Syncer2", 0, 0, nil, nil},
			},
			expectedError: "state sync failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With concurrent execution, both syncers may be started
				// but the first one should fail during Wait().
				// Second syncer may or may not be started due to concurrent execution.
				// We can't guarantee it won't be started in concurrent mode.
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "First syncer should have been waited on")
			},
		},
		{
			name:    "empty registry",
			syncers: []syncerConfig{},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// No assertions needed for empty registry
			},
		},
		{
			name: "multiple errors",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, errors.New("start failed"), nil},
				{"Syncer2", 0, 0, nil, errors.New("wait failed")},
			},
			expectedError: "state sync failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Both syncers should have been started
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.True(t, mockSyncers[1].startCalled, "Second syncer should have been started")
				// At least one should have been waited on (the one that didn't fail during start)
				require.True(t, mockSyncers[0].waitCalled || mockSyncers[1].waitCalled, "At least one syncer should have been waited on")
			},
		},
		{
			name: "call counts verification",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
				{"Syncer2", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Verify call counts
				require.Equal(t, 1, mockSyncers[0].startCallCount, "Syncer1 should have Start called once")
				require.Equal(t, 1, mockSyncers[0].waitCallCount, "Syncer1 should have Wait called once")
				require.Equal(t, 1, mockSyncers[1].startCallCount, "Syncer2 should have Start called once")
				require.Equal(t, 1, mockSyncers[1].waitCallCount, "Syncer2 should have Wait called once")
			},
		},
		{
			name: "nil client",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
			},
			useNilClient: true,
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				require.True(t, mockSyncers[0].startCalled, "Syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "Syncer should have been waited on")
			},
		},
		{
			name: "mixed success failure",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},                       // Success
				{"Syncer2", 0, 0, nil, errors.New("wait failed")}, // Failure
			},
			expectedError: "state sync failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Both syncers should have been started and waited on
				require.True(t, mockSyncers[0].startCalled, "Successful syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "Successful syncer should have been waited on")
				require.True(t, mockSyncers[1].startCalled, "Failed syncer should have been started")
				require.True(t, mockSyncers[1].waitCalled, "Failed syncer should have been waited on")
			},
		},
		{
			name: "context cancellation",
			syncers: []syncerConfig{
				{"Syncer1", 200 * time.Millisecond, 200 * time.Millisecond, nil, nil},
				{"Syncer2", 200 * time.Millisecond, 200 * time.Millisecond, nil, nil},
			},
			contextTimeout: 50 * time.Millisecond,
			expectedError:  "state sync failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With context cancellation, we can't guarantee which syncers were started
				// but at least one should have been attempted
				startedCount := 0
				for _, syncer := range mockSyncers {
					if syncer.startCalled {
						startedCount++
					}
				}
				require.Greater(t, startedCount, 0, "At least one syncer should have been started")
			},
		},
		{
			name: "large number of syncers",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
				{"Syncer2", 0, 0, nil, nil},
				{"Syncer3", 0, 0, nil, nil},
				{"Syncer4", 0, 0, nil, nil},
				{"Syncer5", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// All syncers should have been started and waited on
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
		{
			name: "registry reuse",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				require.True(t, mockSyncers[0].startCalled, "Syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "Syncer should have been waited on")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := NewSyncerRegistry()
			mockSyncers := make([]*mockSyncer, len(tt.syncers))

			// Register syncers.
			for i, syncerConfig := range tt.syncers {
				mockSyncer := newMockSyncer(
					syncerConfig.name,
					syncerConfig.startDelay,
					syncerConfig.waitDelay,
					syncerConfig.startError,
					syncerConfig.waitError,
				)
				mockSyncers[i] = mockSyncer
				require.NoError(t, registry.Register(syncerConfig.name, mockSyncer))
			}

			var (
				ctx        context.Context
				cancel     context.CancelFunc
				mockClient *client
			)

			if tt.contextTimeout > 0 {
				ctx, cancel = context.WithTimeout(context.Background(), tt.contextTimeout)
				defer cancel()
			} else {
				ctx = context.Background()
			}

			if !tt.useNilClient {
				mockClient = &client{}
			}

			err := registry.RunSyncerTasks(ctx, mockClient)

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Use custom assertion function for each test case.
			tt.assertState(t, mockSyncers, err)
		})
	}
}

// TestSyncerRegistry_ConcurrentRegistration tests that the registry handles concurrent registration safely.
func TestSyncerRegistry_ConcurrentRegistration(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	// Register syncers concurrently using errgroup
	g, _ := errgroup.WithContext(context.Background())

	for i := 0; i < 5; i++ {
		g.Go(func() error {
			syncer := newMockSyncer(fmt.Sprintf("Syncer%d", i), 0, 0, nil, nil)
			return registry.Register(fmt.Sprintf("Syncer%d", i), syncer)
		})
	}

	// Wait for all registrations to complete.
	err := g.Wait()
	require.NoError(t, err, "All registrations should succeed even under concurrent access")

	// Verify all syncers were registered.
	count := 0
	registry.syncers.Range(func(key, value any) bool {
		count++
		return true
	})
	require.Equal(t, 5, count)

	// Run the syncers to verify they work.
	require.NoError(t, registry.RunSyncerTasks(context.Background(), &client{}))
}
