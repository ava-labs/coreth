// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP5 defines constants used during the Apricot Phase 5 upgrade.
package cortina

// GasLimit is the maximum amount of gas that can be included in a single block
// after the Cortina upgrade.
//
// This value modifies the previously used `ap4.MinBaseFee`.
const GasLimit = 15_000_000
