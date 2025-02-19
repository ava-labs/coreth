// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"github.com/ava-labs/avalanchego/utils/units"
)

// Minimum Gas Price
const (
	// MinGasPrice is the number of nAVAX required per gas unit for a
	// transaction to be valid, measured in wei
	LaunchMinGasPrice int64 = 470 * GWei

	AvalancheAtomicTxFee = units.MilliAvax
)
