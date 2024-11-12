// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	gethtypes "github.com/ava-labs/libevm/core/types"
)

type (
	// Import these types from the go-ethereum package
	StateAccount = gethtypes.StateAccount
	SlimAccount  = gethtypes.SlimAccount
)

var (
	// Import these functions from the go-ethereum package
	NewEmptyStateAccount = gethtypes.NewEmptyStateAccount
	SlimAccountRLP       = gethtypes.SlimAccountRLP
	FullAccount          = gethtypes.FullAccount
	FullAccountRLP       = gethtypes.FullAccountRLP
)

type isMultiCoin bool

var isMultiCoinPayloads = gethtypes.RegisterExtras[isMultiCoin]()

func IsMultiCoin(a *StateAccount) bool {
	return bool(isMultiCoinPayloads.FromStateAccount(a))
}

func EnableMultiCoin(a *StateAccount) {
	isMultiCoinPayloads.SetOnStateAccount(a, true)
}

// XXX: Should be removed once we use the upstream statedb
func DisableMultiCoin(a *StateAccount) {
	isMultiCoinPayloads.SetOnStateAccount(a, false)
}
