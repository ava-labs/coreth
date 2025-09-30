// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/libevm/core/types"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/pseudo"
)

type isMultiCoin bool

var IsMultiCoinPayloads pseudo.Accessor[types.StateOrSlimAccount, isMultiCoin]

func IsMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return bool(IsMultiCoinPayloads.Get(s))
}
