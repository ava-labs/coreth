// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p256verify

import (
	"github.com/ava-labs/libevm/libevm/precompiles/p256verify"
)

// P256VerifyPrecompile is a stateless precompile that directly uses the libevm p256verify precompile.
// It implements ECDSA signature verification on the P256 curve, as described in RIP-7212.
type P256VerifyPrecompile struct{}

// RequiredGas returns the gas required for the p256verify operation.
func (P256VerifyPrecompile) RequiredGas(input []byte) uint64 {
	return p256verify.Precompile{}.RequiredGas(input)
}

// Run executes the p256verify precompile.
func (P256VerifyPrecompile) Run(input []byte) ([]byte, error) {
	return p256verify.Precompile{}.Run(input)
}
