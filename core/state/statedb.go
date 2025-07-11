// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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

// Package state provides a caching layer atop the Ethereum state trie.
package state

import (
	"math/big"

	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/common"
	ethstate "github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"
)

type StateDB = ethstate.StateDB

var New = ethstate.New

type workerPool struct {
	*utils.BoundedWorkers
}

func (wp *workerPool) Done() {
	// Done is guaranteed to only be called after all work is already complete,
	// so we call Wait for goroutines to finish before returning.
	wp.BoundedWorkers.Wait()
}

func WithConcurrentWorkers(prefetchers int) ethstate.PrefetcherOption {
	pool := &workerPool{
		BoundedWorkers: utils.NewBoundedWorkers(prefetchers),
	}
	return ethstate.WithWorkerPools(func() ethstate.WorkerPool { return pool })
}

// Retrieve the balance from the given address or 0 if object not found
func GetBalanceMultiCoin(s *ethstate.StateDB, addr common.Address, coinID common.Hash) *big.Int {
	NormalizeCoinID(&coinID)
	return s.GetState(addr, coinID, stateconf.SkipStateKeyTransformation()).Big()
}

// AddBalance adds amount to the account associated with addr.
func AddBalanceMultiCoin(s *ethstate.StateDB, addr common.Address, coinID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		s.AddBalance(addr, new(uint256.Int)) // used to cause touch
		return
	}

	if !ethstate.GetExtra(s, customtypes.IsMultiCoinPayloads, addr) {
		ethstate.SetExtra(s, customtypes.IsMultiCoinPayloads, addr, true)
	}

	newAmount := new(big.Int).Add(GetBalanceMultiCoin(s, addr, coinID), amount)
	NormalizeCoinID(&coinID)
	s.SetState(addr, coinID, common.BigToHash(newAmount), stateconf.SkipStateKeyTransformation())
}

// SubBalance subtracts amount from the account associated with addr.
func SubBalanceMultiCoin(s *ethstate.StateDB, addr common.Address, coinID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	// Note: It's not needed to set the IsMultiCoin (extras) flag here, as this
	// call would always be preceded by a call to AddBalanceMultiCoin, which would
	// set the extra flag. Seems we should remove the redundant code.
	if !ethstate.GetExtra(s, customtypes.IsMultiCoinPayloads, addr) {
		ethstate.SetExtra(s, customtypes.IsMultiCoinPayloads, addr, true)
	}
	newAmount := new(big.Int).Sub(GetBalanceMultiCoin(s, addr, coinID), amount)
	NormalizeCoinID(&coinID)
	s.SetState(addr, coinID, common.BigToHash(newAmount), stateconf.SkipStateKeyTransformation())
}

// NormalizeCoinID ORs the 0th bit of the first byte in
// `coinID`, which ensures this bit will be 1 and all other
// bits are left the same.
// This partitions multicoin storage from normal state storage.
func NormalizeCoinID(coinID *common.Hash) {
	coinID[0] |= 0x01
}

// NormalizeStateKey ANDs the 0th bit of the first byte in
// `key`, which ensures this bit will be 0 and all other bits
// are left the same.
// This partitions normal state storage from multicoin storage.
func NormalizeStateKey(key *common.Hash) {
	key[0] &= 0xfe
}
