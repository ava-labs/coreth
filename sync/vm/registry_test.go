// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
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
	startCalled    atomic.Bool
	waitCalled     atomic.Bool
	startCallCount atomic.Int32
	waitCallCount  atomic.Int32
	started        atomic.Bool // Track if already started
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
	if m.started.Load() {
		return synccommon.ErrSyncerAlreadyStarted
	}

	m.started.Store(true)
	m.startCalled.Store(true)
	m.startCallCount.Add(1)
	if m.startDelay > 0 {
		time.Sleep(m.startDelay)
	}
	return m.startError
}

func (m *mockSyncer) Wait(ctx context.Context) error {
	m.waitCalled.Store(true)
	m.waitCallCount.Add(1)
	if m.waitDelay > 0 {
		time.Sleep(m.waitDelay)
	}

	return m.waitError
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
			expectedError: errEmptySyncerName.Error(),
			expectedCount: 0, // Empty name should be rejected.
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

			// Verify all syncers were registered (order is not guaranteed with sync.Map).
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

// TestSyncerRegistry_ConcurrentStartStop tests that the registry handles concurrent start/stop operations safely.
func TestSyncerRegistry_ConcurrentStartStop(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	// Register a syncer first
	syncer := newMockSyncer("TestSyncer", 0, 0, nil, nil)
	require.NoError(t, registry.Register("TestSyncer", syncer))

	// Test concurrent RunSyncerTasks calls
	g, _ := errgroup.WithContext(context.Background())
	results := make([]error, 3)

	for i := 0; i < 3; i++ {
		g.Go(func() error {
			mockClient := &client{
				summary: &mockSummary{
					blockHash: testBlockHash,
					height:    100,
				},
			}
			results[i] = registry.RunSyncerTasks(context.Background(), mockClient)
			return nil
		})
	}

	g.Wait()

	// Only one should succeed, others should fail with errCannotRunSyncerTasksTwice
	successCount := 0
	failureCount := 0
	for _, result := range results {
		if result == nil {
			successCount++
		} else {
			require.ErrorIs(t, result, errCannotRunSyncerTasksTwice)
			failureCount++
		}
	}

	require.Equal(t, 1, successCount, "Exactly one RunSyncerTasks should succeed")
	require.Equal(t, 2, failureCount, "Exactly two RunSyncerTasks should fail")
}

// TestSyncerRegistry_ConcurrentRegisterAfterStart tests that registration is properly blocked after start.
func TestSyncerRegistry_ConcurrentRegisterAfterStart(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	// Start the registry in a goroutine
	startDone := make(chan struct{})
	go func() {
		defer close(startDone)
		mockClient := &client{
			summary: &mockSummary{
				blockHash: testBlockHash,
				height:    100,
			},
		}
		registry.RunSyncerTasks(context.Background(), mockClient)
	}()

	// Wait a bit for the start to begin
	time.Sleep(10 * time.Millisecond)

	// Try to register concurrently after start
	g, _ := errgroup.WithContext(context.Background())
	results := make([]error, 5)

	for i := 0; i < 5; i++ {
		g.Go(func() error {
			syncer := newMockSyncer(fmt.Sprintf("LateSyncer%d", i), 0, 0, nil, nil)
			results[i] = registry.Register(fmt.Sprintf("LateSyncer%d", i), syncer)
			return nil
		})
	}

	g.Wait()
	<-startDone

	// All registrations should fail
	for i, result := range results {
		require.ErrorIs(t, result, errCannotRegisterNewSyncer,
			"Registration %d should fail after start", i)
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
		name          string
		syncers       []syncerConfig
		useNilClient  bool
		useNilSummary bool
		expectedError string
		assertState   func(t *testing.T, mockSyncers []*mockSyncer, err error)
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
					require.True(t, mockSyncer.startCalled.Load(), "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled.Load(), "Syncer %d should have been waited on", i)
				}
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
			name: "call counts verification",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},
				{"Syncer2", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Verify call counts
				require.Equal(t, int32(1), mockSyncers[0].startCallCount.Load(), "Syncer1 should have Start called once")
				require.Equal(t, int32(1), mockSyncers[0].waitCallCount.Load(), "Syncer1 should have Wait called once")
				require.Equal(t, int32(1), mockSyncers[1].startCallCount.Load(), "Syncer2 should have Start called once")
				require.Equal(t, int32(1), mockSyncers[1].waitCallCount.Load(), "Syncer2 should have Wait called once")
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
				require.False(t, mockSyncers[0].startCalled.Load(), "Syncer should not have been started with nil client")
				require.False(t, mockSyncers[0].waitCalled.Load(), "Syncer should not have been waited on with nil client")
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
				require.False(t, mockSyncers[0].startCalled.Load(), "Syncer should not have been started with nil summary")
				require.False(t, mockSyncers[0].waitCalled.Load(), "Syncer should not have been waited on with nil summary")
			},
		},
		{
			name: "mixed success failure",
			syncers: []syncerConfig{
				{"Syncer1", 0, 0, nil, nil},                       // Success
				{"Syncer2", 0, 0, nil, errors.New("wait failed")}, // Failure
			},
			expectedError: "Syncer2 failed: wait failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, err error) {
				// Execution order is not guaranteed due to sync.Map iteration.
				// We only require that the failing syncer (Syncer2) ran and was waited on.
				require.True(t, mockSyncers[1].startCalled.Load(), "Failed syncer should have been started")
				require.True(t, mockSyncers[1].waitCalled.Load(), "Failed syncer should have been waited on")
				// Syncer1 may or may not have run depending on iteration order; if it did start, it must have been waited on.
				if mockSyncers[0].startCalled.Load() {
					require.True(t, mockSyncers[0].waitCalled.Load(), "Successful syncer should have been waited on if started")
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
					require.True(t, mockSyncer.startCalled.Load(), "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled.Load(), "Syncer %d should have been waited on", i)
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

			var mockClient *client
			ctx := context.Background()

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
