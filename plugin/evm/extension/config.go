// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extension

import (
	"errors"

	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers"
)

var (
	errNilConfig              = errors.New("nil extension config")
	errNilNetworkCodec        = errors.New("nil network codec")
	errNilSyncSummaryProvider = errors.New("nil sync summary provider")
	errNilSyncableParser      = errors.New("nil syncable parser")
)

type LeafRequestConfig struct {
	// LeafType is the type of the leaf node
	LeafType message.NodeType
	// MetricName is the name of the metric to use for the leaf request
	MetricName string
	// Handler is the handler to use for the leaf request
	Handler handlers.LeafRequestHandler
}
