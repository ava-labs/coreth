// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"time"

	cmetrics "github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/libevm/metrics"
)

type handlerStats struct {
	// MessageSignatureRequestHandler metrics
	messageSignatureRequest         metrics.Counter
	messageSignatureHit             metrics.Counter
	messageSignatureMiss            metrics.Counter
	messageSignatureRequestDuration metrics.Gauge
	// BlockSignatureRequestHandler metrics
	blockSignatureRequest         metrics.Counter
	blockSignatureHit             metrics.Counter
	blockSignatureMiss            metrics.Counter
	blockSignatureRequestDuration metrics.Gauge
}

func newStats() *handlerStats {
	return &handlerStats{
		messageSignatureRequest:         metrics.NewRegisteredCounter("message_signature_request_count", cmetrics.Registry),
		messageSignatureHit:             metrics.NewRegisteredCounter("message_signature_request_hit", cmetrics.Registry),
		messageSignatureMiss:            metrics.NewRegisteredCounter("message_signature_request_miss", cmetrics.Registry),
		messageSignatureRequestDuration: metrics.NewRegisteredGauge("message_signature_request_duration", cmetrics.Registry),
		blockSignatureRequest:           metrics.NewRegisteredCounter("block_signature_request_count", cmetrics.Registry),
		blockSignatureHit:               metrics.NewRegisteredCounter("block_signature_request_hit", cmetrics.Registry),
		blockSignatureMiss:              metrics.NewRegisteredCounter("block_signature_request_miss", cmetrics.Registry),
		blockSignatureRequestDuration:   metrics.NewRegisteredGauge("block_signature_request_duration", cmetrics.Registry),
	}
}

func (h *handlerStats) IncMessageSignatureRequest() { h.messageSignatureRequest.Inc(1) }
func (h *handlerStats) IncMessageSignatureHit()     { h.messageSignatureHit.Inc(1) }
func (h *handlerStats) IncMessageSignatureMiss()    { h.messageSignatureMiss.Inc(1) }
func (h *handlerStats) UpdateMessageSignatureRequestTime(duration time.Duration) {
	h.messageSignatureRequestDuration.Inc(int64(duration))
}
func (h *handlerStats) IncBlockSignatureRequest() { h.blockSignatureRequest.Inc(1) }
func (h *handlerStats) IncBlockSignatureHit()     { h.blockSignatureHit.Inc(1) }
func (h *handlerStats) IncBlockSignatureMiss()    { h.blockSignatureMiss.Inc(1) }
func (h *handlerStats) UpdateBlockSignatureRequestTime(duration time.Duration) {
	h.blockSignatureRequestDuration.Inc(int64(duration))
}
