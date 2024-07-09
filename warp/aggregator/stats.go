// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"time"

	"github.com/ava-labs/coreth/metrics"
)

type aggregatorStats struct {
	signatureRequested metrics.Counter
	signatureErrored   metrics.Counter
	signatureDuration  metrics.Timer

	aggregateRequested metrics.Counter
	aggregateErrored   metrics.Counter
	aggregateDuration  metrics.Timer
}

func newStats() *aggregatorStats {
	return &aggregatorStats{
		signatureRequested: metrics.GetOrRegisterCounter("signature_requested_count", nil),
		signatureErrored:   metrics.GetOrRegisterCounter("signature_requested_error", nil),
		signatureDuration:  metrics.GetOrRegisterTimer("signature_requested_duration", nil),

		aggregateRequested: metrics.GetOrRegisterCounter("aggregate_requested_count", nil),
		aggregateErrored:   metrics.GetOrRegisterCounter("aggregate_requested_error", nil),
		aggregateDuration:  metrics.GetOrRegisterTimer("aggregate_requested_duration", nil),
	}
}

func (a *aggregatorStats) IncSignatureRequested() { a.signatureRequested.Inc(1) }
func (a *aggregatorStats) IncSignatureErrored()   { a.signatureErrored.Inc(1) }
func (a *aggregatorStats) UpdateSignatureDuration(duration time.Duration) {
	a.signatureDuration.Update(duration)
}

func (a *aggregatorStats) IncAggregateRequested() { a.aggregateRequested.Inc(1) }
func (a *aggregatorStats) IncAggregateErrored()   { a.aggregateErrored.Inc(1) }
func (a *aggregatorStats) UpdateAggregateDuration(duration time.Duration) {
	a.aggregateDuration.Update(duration)
}
