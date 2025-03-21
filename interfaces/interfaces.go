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

// NotFound is returned by API methods if the requested item does not exist.
var NotFound = ethereum.NotFound

// Subscription represents an event subscription where events are
// delivered on a data channel.
type Subscription = ethereum.Subscription

// ChainReader provides access to the blockchain. The methods in this interface access raw
// data from either the canonical chain (when requesting by block number) or any
// blockchain fork that was previously downloaded and processed by the node. The block
// number argument can be nil to select the latest canonical block. Reading block headers
// should be preferred over full blocks whenever possible.
//
// The returned error is NotFound if the requested item does not exist.
type ChainReader = ethereum.ChainReader

// TransactionReader provides access to past transactions and their receipts.
// Implementations may impose arbitrary restrictions on the transactions and receipts that
// can be retrieved. Historic transactions may not be available.
//
// Avoid relying on this interface if possible. Contract logs (through the LogFilterer
// interface) are more reliable and usually safer in the presence of chain
// reorganisations.
//
// The returned error is NotFound if the requested item does not exist.
type TransactionReader = ethereum.TransactionReader

// ChainStateReader wraps access to the state trie of the canonical blockchain. Note that
// implementations of the interface may be unable to return state values for old blocks.
// In many cases, using CallContract can be preferable to reading raw contract storage.
type ChainStateReader = ethereum.ChainStateReader

// CallMsg contains parameters for contract calls.
type CallMsg = ethereum.CallMsg

// A ContractCaller provides contract calls, essentially transactions that are executed by
// the EVM but not mined into the blockchain. ContractCall is a low-level method to
// execute such calls. For applications which are structured around specific contracts,
// the abigen tool provides a nicer, properly typed way to perform calls.
type ContractCaller = ethereum.ContractCaller

// FilterQuery contains options for contract log filtering.
type FilterQuery = ethereum.FilterQuery

// LogFilterer provides access to contract log events using a one-off query or continuous
// event subscription.
//
// Logs received through a streaming query subscription may have Removed set to true,
// indicating that the log was reverted due to a chain reorganisation.
type LogFilterer = ethereum.LogFilterer

// TransactionSender wraps transaction sending. The SendTransaction method injects a
// signed transaction into the pending transaction pool for execution. If the transaction
// was a contract creation, the TransactionReceipt method can be used to retrieve the
// contract address after the transaction has been mined.
//
// The transaction must be signed and have a valid nonce to be included. Consumers of the
// API can use package accounts to maintain local private keys and need can retrieve the
// next available nonce using PendingNonceAt.
type TransactionSender = ethereum.TransactionSender

// GasPricer wraps the gas price oracle, which monitors the blockchain to determine the
// optimal gas price given current fee market conditions.
type GasPricer = ethereum.GasPricer

// GasPricer1559 provides access to the EIP-1559 gas price oracle.
type GasPricer1559 = ethereum.GasPricer1559

// FeeHistoryReader provides access to the fee history oracle.
type FeeHistoryReader = ethereum.FeeHistoryReader

// FeeHistory provides recent fee market data that consumers can use to determine
// a reasonable maxPriorityFeePerGas value.
type FeeHistory = ethereum.FeeHistory

// GasEstimator wraps EstimateGas, which tries to estimate the gas needed to execute a
// specific transaction based on the pending state. There is no guarantee that this is the
// true gas limit requirement as other transactions may be added or removed by miners, but
// it should provide a basis for setting a reasonable default.
type GasEstimator = ethereum.GasEstimator

// A PendingStateEventer provides access to real time notifications about changes to the
// pending state.
type PendingStateEventer = ethereum.PendingStateEventer

// BlockNumberReader provides access to the current block number.
type BlockNumberReader = ethereum.BlockNumberReader

// ChainIDReader provides access to the chain ID.
type ChainIDReader = ethereum.ChainIDReader
