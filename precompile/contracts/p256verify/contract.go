// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p256verify

import (
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/precompiles/p256verify"
)

// P256VerifyPrecompile is a stateful precompile that wraps the libevm p256verify precompile.
// It implements ECDSA signature verification on the P256 curve, as described in RIP-7212.
type P256VerifyPrecompile struct{}

// RequiredGas returns the gas required for the p256verify operation.
func (P256VerifyPrecompile) RequiredGas(input []byte) uint64 {
	return p256verify.Precompile{}.RequiredGas(input)
}

// Run executes the p256verify precompile.
func (P256VerifyPrecompile) Run(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Calculate gas cost
	gasCost := p256verify.Precompile{}.RequiredGas(input)
	if suppliedGas < gasCost {
		return nil, 0, vm.ErrOutOfGas
	}
	remainingGas = suppliedGas - gasCost

	// Execute the p256verify precompile
	ret, err = p256verify.Precompile{}.Run(input)
	return ret, remainingGas, err
}
