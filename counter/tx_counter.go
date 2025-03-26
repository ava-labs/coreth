// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package counter

import (
	"sync/atomic"

	"github.com/ava-labs/coreth/core/types"
)

// TxCounter is a simple counter for transactions included in blocks.
type TxCounter struct {
	counter uint64
}

// NewTxCounter creates a new transaction counter.
func NewTxCounter() *TxCounter {
	return &TxCounter{
		counter: 0,
	}
}

// Increment increments the counter and returns the new value.
func (tc *TxCounter) Increment(tx *types.Transaction) uint64 {
	return atomic.AddUint64(&tc.counter, 1)
}

// Get returns the current value of the counter.
func (tc *TxCounter) Get() uint64 {
	return atomic.LoadUint64(&tc.counter)
}
