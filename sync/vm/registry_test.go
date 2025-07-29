// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockSyncer implements synccommon.Syncer for testing
type mockSyncer struct {
	name           string
	startDelay     time.Duration
	waitDelay      time.Duration
	startError     error
	waitError      error
	startCalled    bool
	waitCalled     bool
	startCallCount int
	waitCallCount  int
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
	m.startCalled = true
	m.startCallCount++
	if m.startDelay > 0 {
		time.Sleep(m.startDelay)
	}
	return m.startError
}

func (m *mockSyncer) Wait(ctx context.Context) error {
	m.waitCalled = true
	m.waitCallCount++
	if m.waitDelay > 0 {
		time.Sleep(m.waitDelay)
	}
	return m.waitError
}

func TestNewSyncerRegistry(t *testing.T) {
	registry := NewSyncerRegistry()
	require.NotNil(t, registry)
	require.Empty(t, registry.syncers)
}

func TestSyncerRegistry_Register(t *testing.T) {
	registry := NewSyncerRegistry()

	syncer1 := newMockSyncer("TestSyncer1", 0, 0, nil, nil)
	syncer2 := newMockSyncer("TestSyncer2", 0, 0, nil, nil)

	require.NoError(t, registry.Register("Syncer1", syncer1))
	require.NoError(t, registry.Register("Syncer2", syncer2))

	require.Len(t, registry.syncers, 2)
	require.Equal(t, "Syncer1", registry.syncers[0].name)
	require.Equal(t, syncer1, registry.syncers[0].syncer)
	require.Equal(t, "Syncer2", registry.syncers[1].name)
	require.Equal(t, syncer2, registry.syncers[1].syncer)
}

func TestSyncerRegistry_Register_DuplicateName(t *testing.T) {
	registry := NewSyncerRegistry()

	syncer1 := newMockSyncer("TestSyncer", 0, 0, nil, nil)
	syncer2 := newMockSyncer("TestSyncer", 0, 0, nil, nil)

	// First registration should succeed.
	require.NoError(t, registry.Register("TestSyncer", syncer1))
	require.Len(t, registry.syncers, 1)

	// Second registration with same name should fail.
	err := registry.Register("TestSyncer", syncer2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "syncer with name 'TestSyncer' is already registered")
	require.Len(t, registry.syncers, 1) // Should not have added the duplicate.
}

func TestSyncerRegistry_RegistrationOrder(t *testing.T) {
	registry := NewSyncerRegistry()

	syncer1 := newMockSyncer("Syncer1", 0, 0, nil, nil)
	syncer2 := newMockSyncer("Syncer2", 0, 0, nil, nil)
	syncer3 := newMockSyncer("Syncer3", 0, 0, nil, nil)

	// Register in specific order
	require.NoError(t, registry.Register("Syncer1", syncer1))
	require.NoError(t, registry.Register("Syncer2", syncer2))
	require.NoError(t, registry.Register("Syncer3", syncer3))

	// Verify syncers are in registration order
	require.Equal(t, "Syncer1", registry.syncers[0].name)
	require.Equal(t, "Syncer2", registry.syncers[1].name)
	require.Equal(t, "Syncer3", registry.syncers[2].name)
}

func TestSyncerRegistry_EmptyExecution(t *testing.T) {
	registry := NewSyncerRegistry()

	// Test that empty registry doesn't cause issues
	require.Len(t, registry.syncers, 0)
}

func TestSyncerRegistry_SyncerLifecycle(t *testing.T) {
	// Test individual syncer lifecycle
	syncer := newMockSyncer("TestSyncer", 0, 0, nil, nil)

	ctx := context.Background()

	// Test successful execution
	require.NoError(t, syncer.Start(ctx))
	require.True(t, syncer.startCalled)
	require.Equal(t, 1, syncer.startCallCount)

	require.NoError(t, syncer.Wait(ctx))
	require.True(t, syncer.waitCalled)
	require.Equal(t, 1, syncer.waitCallCount)
}

func TestSyncerRegistry_SyncerErrors(t *testing.T) {
	// Test syncer error handling
	startError := errors.New("start failed")
	waitError := errors.New("wait failed")

	syncer := newMockSyncer("TestSyncer", 0, 0, startError, waitError)

	ctx := context.Background()

	// Test start error
	err := syncer.Start(ctx)
	require.Error(t, err)
	require.Equal(t, startError, err)

	// Test wait error
	err = syncer.Wait(ctx)
	require.Error(t, err)
	require.Equal(t, waitError, err)
}

func TestSyncerRegistry_SyncerDelays(t *testing.T) {
	// Test syncer with delays
	syncer := newMockSyncer("TestSyncer", 10*time.Millisecond, 20*time.Millisecond, nil, nil)

	ctx := context.Background()
	start := time.Now()

	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	duration := time.Since(start)

	// Should take at least the sum of delays
	require.GreaterOrEqual(t, duration, 30*time.Millisecond)
}
