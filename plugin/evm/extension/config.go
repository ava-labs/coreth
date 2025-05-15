// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extension

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ava-labs/coreth/sync/handlers"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

var (
	errNilConfig              = errors.New("nil extension config")
	errNilNetworkCodec        = errors.New("nil network codec")
	errNilSyncSummaryProvider = errors.New("nil sync summary provider")
	errNilSyncableParser      = errors.New("nil syncable parser")
)
k and syncer
type LeafRequestConfig struct {
	// LeafType is the type of the leaf node
	LeafType message.NodeType
	// MetricName is the name of the metric to use for the leaf request
	MetricName string
	// Handler is the handler to use for the leaf request
	Handler handlers.LeafRequestHandler
}

