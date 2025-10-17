package customtypes

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

type isMultiCoin bool

var IsMultiCoinPayloads = extras.StateAccount

func IsMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return bool(IsMultiCoinPayloads.Get(s))
}
