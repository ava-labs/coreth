// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"time"

	"github.com/ava-labs/coreth/plugin/evm/message"
)

var _ ClientSyncerStats = &noopStats{}

// no-op implementation of ClientSyncerStats
type noopStats struct {
	noop noopMsgMetric
}

type noopMsgMetric struct{}

func (noopMsgMetric) IncRequested()                      {}
func (noopMsgMetric) IncSucceeded()                      {}
func (noopMsgMetric) IncFailed()                         {}
func (noopMsgMetric) IncInvalidResponse()                {}
func (noopMsgMetric) UpdateReceived(int64)               {}
func (noopMsgMetric) UpdateRequestLatency(time.Duration) {}

func NewNoOpStats() ClientSyncerStats {
	return &noopStats{}
}

func (n noopStats) GetMetric(_ message.Request) (MessageMetric, error) {
	return n.noop, nil
}

// all operations are no-ops
func (n *noopStats) IncLeavesRequested()                    {}
func (n *noopStats) UpdateLeavesReceived(int64)             {}
func (n *noopStats) UpdateLeafRequestLatency(time.Duration) {}
