// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"time"

	"github.com/ethereum/go-ethereum/metrics"
)

type Stats interface {
	IncLeavesRequested()
	UpdateLeavesReceived(size int64)

	UpdateTrieCommitted(size int64)
	UpdateStorageCommitted(size int64)
	UpdateCodeCommitted(size int64)

	UpdateLeafRequestLatency(duration time.Duration)
}

type stateSyncerStats struct {
	// network metrics
	leavesRequested metrics.Counter
	leavesReceived  metrics.Histogram

	// writes
	leavesCommitted  metrics.Histogram
	storageCommitted metrics.Histogram
	codeCommitted    metrics.Histogram

	// latency
	leafRequestLatency metrics.Timer
}

func NewStats() Stats {
	return &stateSyncerStats{
		leavesRequested: metrics.GetOrRegisterCounter("sync_leaves_requested", nil),
		leavesReceived:  metrics.GetOrRegisterHistogram("sync_leaves_received", nil, metrics.NewExpDecaySample(1028, 0.015)),

		leavesCommitted:  metrics.GetOrRegisterHistogram("sync_leaves_committed", nil, metrics.NewExpDecaySample(1028, 0.015)),
		storageCommitted: metrics.GetOrRegisterHistogram("sync_storage_committed", nil, metrics.NewExpDecaySample(1028, 0.015)),
		codeCommitted:    metrics.GetOrRegisterHistogram("sync_code_committed", nil, metrics.NewExpDecaySample(1028, 0.015)),

		leafRequestLatency: metrics.GetOrRegisterTimer("sync_leaf_request_latency", nil),
	}
}

func (s *stateSyncerStats) IncLeavesRequested() {
	s.leavesRequested.Inc(1)
}

func (s *stateSyncerStats) UpdateLeavesReceived(size int64) {
	s.leavesReceived.Update(size)
}

func (s *stateSyncerStats) UpdateTrieCommitted(bytes int64) {
	s.leavesCommitted.Update(bytes)
}

func (s *stateSyncerStats) UpdateStorageCommitted(bytes int64) {
	s.storageCommitted.Update(bytes)
}

func (s *stateSyncerStats) UpdateCodeCommitted(bytes int64) {
	s.codeCommitted.Update(bytes)
}

func (s *stateSyncerStats) UpdateLeafRequestLatency(duration time.Duration) {
	s.leafRequestLatency.Update(duration)
}

type noopStats struct{}

func NewNoOpStats() Stats {
	return &noopStats{}
}

// all operations are no-ops
func (n *noopStats) IncLeavesRequested()                    {}
func (n *noopStats) UpdateLeavesReceived(int64)             {}
func (n *noopStats) UpdateTrieCommitted(int64)              {}
func (n *noopStats) UpdateStorageCommitted(int64)           {}
func (n *noopStats) UpdateCodeCommitted(int64)              {}
func (n *noopStats) UpdateLeafRequestLatency(time.Duration) {}
