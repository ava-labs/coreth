// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
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

// Package ethereum defines interfaces for interacting with Ethereum.
package interfaces

import (
	ethereum "github.com/ava-labs/libevm"
)

var NotFound = ethereum.NotFound

type Subscription = ethereum.Subscription

type ChainReader = ethereum.ChainReader

type TransactionReader = ethereum.TransactionReader

type ChainStateReader = ethereum.ChainStateReader

type CallMsg = ethereum.CallMsg

type ContractCaller = ethereum.ContractCaller

type FilterQuery = ethereum.FilterQuery

type LogFilterer = ethereum.LogFilterer

type TransactionSender = ethereum.TransactionSender

type GasPricer = ethereum.GasPricer

type GasPricer1559 = ethereum.GasPricer1559

type FeeHistoryReader = ethereum.FeeHistoryReader

type FeeHistory = ethereum.FeeHistory

type GasEstimator = ethereum.GasEstimator

type PendingStateEventer = ethereum.PendingStateEventer

type BlockNumberReader = ethereum.BlockNumberReader

type ChainIDReader = ethereum.ChainIDReader
