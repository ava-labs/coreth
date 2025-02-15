// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP5 defines constants used during the Apricot Phase 5 upgrade.
package etna

import "github.com/ava-labs/coreth/params"

// MinBaseFee is the minimum base fee that is allowed after the Etna upgrade.
//
// This value modifies the previously used `ap4.MinBaseFee`.
const MinBaseFee = params.GWei
