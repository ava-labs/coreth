// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/libevm/libevm"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var extras ethtypes.ExtraPayloads[*HeaderExtra, *BlockBodyExtra, isMultiCoin]

func setExtras(e ethtypes.ExtraPayloads[*HeaderExtra, *BlockBodyExtra, isMultiCoin]) {
	extras = e
	IsMultiCoinPayloads = e.StateAccount
}

// Register registers the types with libevm. It MUST NOT be called more than
// once and therefore is only allowed to be used in tests and `package main`, to
// avoid polluting other packages that transitively depend on this one but don't
// need registration.
//
// Without a call to Register, none of the functionality of this package will
// work, and most will simply panic.
func Register() {
	setExtras(ethtypes.RegisterExtras[
		HeaderExtra, *HeaderExtra,
		BlockBodyExtra, *BlockBodyExtra,
		isMultiCoin,
	]())
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
//
// This function is not intended for direct use. Use
// `evm.WithTempRegisteredLibEVMExtras()` instead as it calls this along with
// all other temporary-registration functions.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	old := extras
	defer setExtras(old)

	return ethtypes.WithTempRegisteredExtras[HeaderExtra, BlockBodyExtra, isMultiCoin](
		lock,
		func(e ethtypes.ExtraPayloads[*HeaderExtra, *BlockBodyExtra, isMultiCoin]) error {
			setExtras(e)
			return fn()
		},
	)
}
