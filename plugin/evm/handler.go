// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
)

var _ p2p.Handler = (*validatorHandler)(nil)

// validatorHandler drops messages from non-validators
// TODO return an application-level error to non-validators when sdk supports it
type validatorHandler struct {
	validators *p2p.Validators
	handler    p2p.Handler
}

func (v validatorHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) error {
	if !v.validators.Has(ctx, nodeID) {
		return nil
	}

	return v.handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (v validatorHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if !v.validators.Has(ctx, nodeID) {
		return nil, nil
	}

	return v.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

func (v validatorHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return v.handler.CrossChainAppRequest(ctx, chainID, deadline, requestBytes)
}
