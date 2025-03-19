// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

var SkipRegisterExtras = false

var extras ethtypes.ExtraPayloads[*HeaderExtra, *BlockBodyExtra, isMultiCoin]

func init() {
	if SkipRegisterExtras {
		return
	} else {
		extras = ethtypes.RegisterExtras[
			HeaderExtra, *HeaderExtra,
			BlockBodyExtra, *BlockBodyExtra,
			isMultiCoin,
		]()
	}

}
