// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// SignatureGetter defines the minimum network interface to perform signature aggregation
type SignatureGetter interface {
	// GetSignature attempts to fetch a BLS Signature from [nodeID] for [unsignedWarpMessage]
	GetSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error)
}

// TODO: this is unused, but generates a mock, which will fail lint
type NetworkClient interface {
	SendSyncedAppRequest(ctx context.Context, nodeID ids.NodeID, message []byte) ([]byte, error)
}
