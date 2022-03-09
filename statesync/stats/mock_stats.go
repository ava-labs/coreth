// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"sync"
	"time"
)

var _ Stats = &MockSyncerStats{}

// MockSyncerStats is stats collector for metrics
// Fields are exported here because it is expected that they are accessed
// when the syncer has finished running in test so there will be no race condition
type MockSyncerStats struct {
	lock                  sync.Mutex
	LeavesRequested       uint32
	LeavesReceived        int64
	TrieCommitted         int64
	StorageCommitted      int64
	CodeCommitted         int64
	LeafRequestLatencySum time.Duration
}

func (m *MockSyncerStats) IncLeavesRequested() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeavesRequested++
}

func (m *MockSyncerStats) IncLeavesReceived(size int64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeavesReceived += size
}

func (m *MockSyncerStats) UpdateTrieCommitted(size int64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.TrieCommitted += size
}

func (m *MockSyncerStats) UpdateStorageCommitted(size int64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.StorageCommitted += size
}

func (m *MockSyncerStats) UpdateCodeCommitted(size int64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeCommitted += size
}

func (m *MockSyncerStats) UpdateLeafRequestLatency(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafRequestLatencySum += duration
}
