// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/plugin/evm/message"
	synccommon "github.com/ava-labs/coreth/sync"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var testBlockHash = common.HexToHash("0xdeadbeef")

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
	waitResult     synccommon.WaitResult
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

// newMockSyncerWithWaitResult creates a mock syncer that returns a specific WaitResult.
func newMockSyncerWithWaitResult(name string, startDelay, waitDelay time.Duration, startError error, waitResult synccommon.WaitResult) *mockSyncer {
	return &mockSyncer{
		name:       name,
		startDelay: startDelay,
		waitDelay:  waitDelay,
		startError: startError,
		waitResult: waitResult,
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

func (m *mockSyncer) Wait(ctx context.Context) synccommon.WaitResult {
	m.waitCalled = true
	m.waitCallCount++
	if m.waitDelay > 0 {
		select {
		case <-time.After(m.waitDelay):
		case <-ctx.Done():
			return synccommon.WaitResult{Err: ctx.Err(), Cancelled: true}
		}
	}

	return m.waitResult
}

type mockSummary struct {
	blockHash common.Hash
	height    uint64
}

func (m *mockSummary) GetBlockHash() common.Hash {
	return m.blockHash
}

func (m *mockSummary) Height() uint64 {
	return m.height
}

func (m *mockSummary) GetBlockRoot() common.Hash {
	return m.blockHash // Use blockHash as root for simplicity.
}

func (m *mockSummary) Accept(context.Context) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, nil
}

func (m *mockSummary) Bytes() []byte {
	return []byte("mock summary")
}

func (m *mockSummary) ID() ids.ID {
	return ids.FromStringOrPanic("mock-summary-id")
}

// Interface checks to ensure mocks implement the expected interfaces.
var (
	_ synccommon.Syncer = (*mockSyncer)(nil)
	_ message.Syncable  = (*mockSummary)(nil)
)

func TestNewSyncerRegistry(t *testing.T) {
	t.Parallel()
	registry := NewSyncerRegistry()
	require.NotNil(t, registry)

	// Check that the registry is empty.
	count := 0
	registry.syncers.Range(func(key, value any) bool {
		count++
		return true
	})
	require.Equal(t, 0, count)
}

func TestSyncerRegistry_Register(t *testing.T) {
	t.Parallel()

	type registration struct {
		name   string
		syncer *mockSyncer
	}

	type testCase struct {
		name          string
		registrations []registration
		expectedError string
		expectedCount int
	}

	tests := []testCase{
		{
			name: "successful registrations",
			registrations: []registration{
				{"Syncer1", newMockSyncer("TestSyncer1", 0, 0, nil, nil)},
				{"Syncer2", newMockSyncer("TestSyncer2", 0, 0, nil, nil)},
			},
			expectedError: "",
			expectedCount: 2,
		},
		{
			name: "duplicate name registration",
			registrations: []registration{
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
			},
			expectedError: "syncer with name 'Syncer1' is already registered",
			expectedCount: 1,
		},
		{
			name: "preserve registration order",
			registrations: []registration{
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
				{"Syncer2", newMockSyncer("Syncer2", 0, 0, nil, nil)},
				{"Syncer3", newMockSyncer("Syncer3", 0, 0, nil, nil)},
			},
			expectedCount: 3,
		},
		{
			name: "empty name registration",
			registrations: []registration{
				{"", newMockSyncer("EmptyName", 0, 0, nil, nil)},
			},
			expectedCount: 1, // Empty name should be allowed
		},
		{
			name: "nil syncer registration",
			registrations: []registration{
				{"Syncer1", nil},
			},
			expectedError: errSyncerCannotBeNil.Error(),
			expectedCount: 0,
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
		useNilSummary  bool
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
			expectedError: "sync start failed",
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
			expectedError: "sync execution failed",
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
			expectedError: "sync start failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With the two-phase (start/wait) approach, if the first syncer fails to start,
				// the second syncer may or may not be started due to concurrent execution.
				// We can only guarantee that at least one syncer was attempted.
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
			useNilClient:  true,
			expectedError: errClientCannotProvideSummary.Error(),
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With nil client, syncers should not be started
				require.False(t, mockSyncers[0].startCalled, "Syncer should not have been started with nil client")
				require.False(t, mockSyncers[0].waitCalled, "Syncer should not have been waited on with nil client")
			},
		},
		{
			name: "nil summary",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
			},
			useNilClient:  false,
			useNilSummary: true,
			expectedError: errClientCannotProvideSummary.Error(),
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With nil summary, syncers should not be started
				require.False(t, mockSyncers[0].startCalled, "Syncer should not have been started with nil summary")
				require.False(t, mockSyncers[0].waitCalled, "Syncer should not have been waited on with nil summary")
			},
		},
		{
			name: "mixed success failure",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},                       // Success
				{"Syncer2", 0, 0, nil, errors.New("wait failed")}, // Failure
			},
			expectedError: "sync execution failed",
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
			expectedError:  "", // Registry is resilient to context cancellation
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// With context cancellation, the registry detaches from the cancellation
				// and allows syncers to complete. All syncers should be started and waited on.
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
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
				{"Syncer6", 0, 0, nil, nil},
				{"Syncer7", 0, 0, nil, nil},
				{"Syncer8", 0, 0, nil, nil},
				{"Syncer9", 0, 0, nil, nil},
				{"Syncer10", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// All syncers should have been started and waited on
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
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
				if tt.useNilSummary {
					mockClient = &client{
						summary: nil,
					}
				} else {
					mockClient = &client{
						summary: &mockSummary{
							blockHash: testBlockHash,
							height:    100,
						},
					}
				}
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

// TestSyncerRegistry_LifecycleScenarios tests the lifecycle of the syncer registry.
func TestSyncerRegistry_LifecycleScenarios(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name                 string
		registerBeforeRun    bool // if true we register a syncer before the first run
		expectRegisterError  bool
		expectSecondRunError bool
	}

	tests := []testCase{
		{
			name:                 "late Register is rejected",
			registerBeforeRun:    true,
			expectRegisterError:  true,
			expectSecondRunError: true,
		},
		{
			name:                 "second RunSyncerTasks is rejected",
			registerBeforeRun:    true,
			expectRegisterError:  true,
			expectSecondRunError: true,
		},
		{
			name:                 "normal lifecycle succeeds",
			registerBeforeRun:    true,
			expectRegisterError:  true, // late Register still fails
			expectSecondRunError: true, // second run still fails
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := NewSyncerRegistry()

			// Optionally register one syncer *before* running.
			if tt.registerBeforeRun {
				require.NoError(t,
					registry.Register("Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)),
				)
			}

			ctx := context.Background()
			mockClient := &client{
				summary: &mockSummary{
					blockHash: testBlockHash,
					height:    42,
				},
			}

			// First run should always succeed.
			require.NoError(t, registry.RunSyncerTasks(ctx, mockClient))

			// Late Register attempt.
			err := registry.Register("LateSyncer", newMockSyncer("LateSyncer", 0, 0, nil, nil))
			if tt.expectRegisterError {
				require.ErrorIs(t, err, errCannotRegisterNewSyncer, "expect error when registering after RunSyncerTasks")
			} else {
				require.NoError(t, err)
			}

			// Second RunSyncerTasks attempt.
			err = registry.RunSyncerTasks(ctx, mockClient)
			if tt.expectSecondRunError {
				require.ErrorIs(t, err, errCannotRunSyncerTasksTwice, "expect error when running syncer tasks again")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestSyncerRegistry_ConcurrentRegistration tests that the registry handles concurrent registration safely.
func TestSyncerRegistry_ConcurrentRegistration(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	// Register syncers concurrently using errgroup.
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
	require.NoError(t, registry.RunSyncerTasks(context.Background(), &client{
		summary: &mockSummary{
			blockHash: testBlockHash,
			height:    100,
		},
	}))
}

// TestSyncerRegistry_WaitResultPattern tests the new WaitResult pattern with various cancellation scenarios.
func TestSyncerRegistry_WaitResultPattern(t *testing.T) {
	t.Parallel()

	type waitResultConfig struct {
		name       string
		startDelay time.Duration
		waitDelay  time.Duration
		startError error
		waitResult synccommon.WaitResult
	}

	type testCase struct {
		name           string
		syncers        []waitResultConfig
		contextTimeout time.Duration
		expectedError  string
		assertState    func(t *testing.T, mockSyncers []*mockSyncer, err error)
	}

	tests := []testCase{
		{
			name: "clean cancellation - all syncers cancelled",
			syncers: []waitResultConfig{
				{"Syncer1", 0, 200 * time.Millisecond, nil, synccommon.WaitResult{Cancelled: true, Err: nil}},
				{"Syncer2", 0, 200 * time.Millisecond, nil, synccommon.WaitResult{Cancelled: true, Err: nil}},
			},
			contextTimeout: 50 * time.Millisecond,
			expectedError:  "context deadline exceeded", // Registry propagates context cancellation.
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// All syncers should be started and waited on, but cancelled due to context timeout.
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
		{
			name: "mixed cancellation and errors",
			syncers: []waitResultConfig{
				{"Syncer1", 0, 0, nil, synccommon.WaitResult{Cancelled: true, Err: nil}},                       // Clean cancellation
				{"Syncer2", 0, 0, nil, synccommon.WaitResult{Cancelled: false, Err: errors.New("real error")}}, // Real error
			},
			expectedError: "sync execution failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Both syncers should be started and waited on.
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "First syncer should have been waited on")
				require.True(t, mockSyncers[1].startCalled, "Second syncer should have been started")
				require.True(t, mockSyncers[1].waitCalled, "Second syncer should have been waited on")
			},
		},
		{
			name: "context cancellation with WaitResult",
			syncers: []waitResultConfig{
				{"Syncer1", 0, 200 * time.Millisecond, nil, synccommon.WaitResult{Cancelled: true, Err: context.Canceled}},
				{"Syncer2", 0, 200 * time.Millisecond, nil, synccommon.WaitResult{Cancelled: true, Err: context.Canceled}},
			},
			contextTimeout: 50 * time.Millisecond,
			expectedError:  "context deadline exceeded", // Registry propagates context cancellation.
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// All syncers should be started and waited on, but cancelled due to context.
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
		{
			name: "partial cancellation - some cancelled, some succeed",
			syncers: []waitResultConfig{
				{"Syncer1", 0, 0, nil, synccommon.WaitResult{Cancelled: true, Err: nil}},  // Cancelled
				{"Syncer2", 0, 0, nil, synccommon.WaitResult{Cancelled: false, Err: nil}}, // Success
			},
			expectedError: "", // Registry should succeed when some syncers are cancelled cleanly.
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Both syncers should be started and waited on.
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "First syncer should have been waited on")
				require.True(t, mockSyncers[1].startCalled, "Second syncer should have been started")
				require.True(t, mockSyncers[1].waitCalled, "Second syncer should have been waited on")
			},
		},
		{
			name: "all syncers succeed with WaitResult",
			syncers: []waitResultConfig{
				{"Syncer1", 0, 0, nil, synccommon.WaitResult{Cancelled: false, Err: nil}},
				{"Syncer2", 0, 0, nil, synccommon.WaitResult{Cancelled: false, Err: nil}},
			},
			expectedError: "",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// All syncers should be started and waited on successfully.
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
		{
			name: "deadline exceeded with WaitResult",
			syncers: []waitResultConfig{
				{"Syncer1", 0, 200 * time.Millisecond, nil, synccommon.WaitResult{Cancelled: true, Err: context.DeadlineExceeded}},
				{"Syncer2", 0, 200 * time.Millisecond, nil, synccommon.WaitResult{Cancelled: true, Err: context.DeadlineExceeded}},
			},
			contextTimeout: 50 * time.Millisecond,
			expectedError:  "context deadline exceeded", // Registry propagates context cancellation.
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// All syncers should be started and waited on, but cancelled due to deadline.
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := NewSyncerRegistry()
			mockSyncers := make([]*mockSyncer, len(tt.syncers))

			// Register syncers with WaitResult configuration.
			for i, syncerConfig := range tt.syncers {
				mockSyncer := newMockSyncerWithWaitResult(
					syncerConfig.name,
					syncerConfig.startDelay,
					syncerConfig.waitDelay,
					syncerConfig.startError,
					syncerConfig.waitResult,
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

			mockClient = &client{
				summary: &mockSummary{
					blockHash: testBlockHash,
					height:    100,
				},
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

// TestSyncerRegistry_WaitResultEdgeCases tests edge cases of the WaitResult pattern.
func TestSyncerRegistry_WaitResultEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		waitResult    synccommon.WaitResult
		expectedError string
		description   string
	}{
		{
			name:          "cancelled with nil error",
			waitResult:    synccommon.WaitResult{Cancelled: true, Err: nil},
			expectedError: "",
			description:   "Clean cancellation should be treated as success",
		},
		{
			name:          "cancelled with context.Canceled error",
			waitResult:    synccommon.WaitResult{Cancelled: true, Err: context.Canceled},
			expectedError: "",
			description:   "Context cancellation should be treated as success",
		},
		{
			name:          "cancelled with context.DeadlineExceeded error",
			waitResult:    synccommon.WaitResult{Cancelled: true, Err: context.DeadlineExceeded},
			expectedError: "",
			description:   "Deadline exceeded should be treated as success",
		},
		{
			name:          "not cancelled with real error",
			waitResult:    synccommon.WaitResult{Cancelled: false, Err: errors.New("real error")},
			expectedError: "sync execution failed",
			description:   "Real errors should cause sync failure",
		},
		{
			name:          "not cancelled with nil error",
			waitResult:    synccommon.WaitResult{Cancelled: false, Err: nil},
			expectedError: "",
			description:   "Successful completion should succeed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := NewSyncerRegistry()

			// Create a mock syncer that returns the specific WaitResult.
			mockSyncer := newMockSyncerWithWaitResult("TestSyncer", 0, 0, nil, tt.waitResult)
			require.NoError(t, registry.Register("TestSyncer", mockSyncer))

			mockClient := &client{
				summary: &mockSummary{
					blockHash: testBlockHash,
					height:    100,
				},
			}

			err := registry.RunSyncerTasks(context.Background(), mockClient)

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Verify the syncer was started and waited on.
			require.True(t, mockSyncer.startCalled, "Syncer should have been started")
			require.True(t, mockSyncer.waitCalled, "Syncer should have been waited on")
		})
	}
}

// TestSyncerRegistry_ContextCancellation tests the registry's handling of parent context cancellation.
func TestSyncerRegistry_ContextCancellation(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	// Register syncers that will take some time to complete.
	syncer1 := newMockSyncer("Syncer1", 0, 200*time.Millisecond, nil, nil)
	syncer2 := newMockSyncer("Syncer2", 0, 200*time.Millisecond, nil, nil)

	require.NoError(t, registry.Register("Syncer1", syncer1))
	require.NoError(t, registry.Register("Syncer2", syncer2))

	// Create a context that will be cancelled quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	mockClient := &client{
		summary: &mockSummary{
			blockHash: testBlockHash,
			height:    100,
		},
	}

	// Run the registry with a context that will be cancelled.
	err := registry.RunSyncerTasks(ctx, mockClient)

	// The registry should propagate context cancellation errors.
	require.Error(t, err, "Registry should propagate context cancellation errors")
	require.Contains(t, err.Error(), "context deadline exceeded")

	// Both syncers should have been started and waited on.
	require.True(t, syncer1.startCalled, "Syncer1 should have been started")
	require.True(t, syncer1.waitCalled, "Syncer1 should have been waited on")
	require.True(t, syncer2.startCalled, "Syncer2 should have been started")
	require.True(t, syncer2.waitCalled, "Syncer2 should have been waited on")
}
