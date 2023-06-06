// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"github.com/ava-labs/avalanchego/ids"
)

// Tx is a transaction that can be gossiped across the network.
type Tx interface {
	// ID returns the unique id of this transaction
	ID() ids.ID
	// Marshal returns the byte representation of this transaction
	Marshal() ([]byte, error)
	// Unmarshal deserializes the provided bytes in-place
	Unmarshal(b []byte) error
}

// Mempool holds pending transactions
type Mempool[T Tx] interface {
	// AddTx adds a transaction to the mempool
	AddTx(tx T, local bool) (bool, error)
	// GetTxs returns transactions that match the provided filter function
	GetTxs(filter func(tx T) bool) []T
	// GetBloomFilter returns a bloom filter representing the transactions in
	// the mempool
	GetBloomFilter() ([]byte, error)
}
