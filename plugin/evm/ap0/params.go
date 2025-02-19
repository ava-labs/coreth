// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP0 defines constants used during the initial network launch.
package ap0

import (
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/params"
)

const (
	// MinGasPrice is the minimum gas price of a transaction at the start of the
	// network.
	//
	// This value was modified by `ap1.MinGasPrice`.
	MinGasPrice = 470 * params.GWei

	// GasLimit is the target amount of gas that can be included in a single
	// block after the Apricot Phase 1 upgrade.
	//
	// This value encodes the default parameterization of the initial gas
	// targeting mechanism.
	AtomicTxFee = units.MilliAvax
)
