// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	cmetrics "github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/libevm/metrics"
)

type verifierStats struct {
	messageParseFail metrics.Counter
	// BlockRequest metrics
	blockValidationFail metrics.Counter
}

func newVerifierStats() *verifierStats {
	return &verifierStats{
		messageParseFail:    metrics.NewRegisteredCounter("warp_backend_message_parse_fail", cmetrics.Registry),
		blockValidationFail: metrics.NewRegisteredCounter("warp_backend_block_validation_fail", cmetrics.Registry),
	}
}

func (h *verifierStats) IncBlockValidationFail() {
	h.blockValidationFail.Inc(1)
}

func (h *verifierStats) IncMessageParseFail() {
	h.messageParseFail.Inc(1)
}
