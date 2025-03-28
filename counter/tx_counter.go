// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package counter

import (
	"sync"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

type TxCounter struct {
	mu    sync.Mutex
	txNum uint64
	txMap map[common.Hash]uint64
}

func NewTxCounter() *TxCounter {
	return &TxCounter{
		txNum: 0,
		txMap: make(map[common.Hash]uint64),
	}
}

func (tc *TxCounter) Increment() uint64 {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.txNum++
	return tc.txNum
}

func (tc *TxCounter) AddTx(tx *types.Transaction) (*types.Transaction, uint64) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.txNum++
	tc.txMap[tx.Hash()] = tc.txNum
	return tx, tc.txNum
}

func (tc *TxCounter) HasTxNum(txHash common.Hash) bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	_, exists := tc.txMap[txHash]
	return exists
}

func (tc *TxCounter) Get(txHash common.Hash) (uint64, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	txNum, exists := tc.txMap[txHash]
	return txNum, exists
}
