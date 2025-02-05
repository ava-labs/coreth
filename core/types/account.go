// Copyright 2024 The go-ethereum Authors
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
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// Account represents an Ethereum account and its attached data.
// This type is used to specify accounts in the genesis block state, and
// is also useful for JSON encoding/decoding of accounts.
type Account = ethtypes.Account

// GenesisAlloc specifies the initial state of a genesis block.
type GenesisAlloc = ethtypes.GenesisAlloc
