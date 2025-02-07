// (c) 2025, Ava Labs, Inc.

package core

import "math/big"

// BlockContext implements the `contract.ConfigurationBlockContext` interface.
type BlockContext struct {
	number    *big.Int
	timestamp uint64
}

// NewBlockContext creates a new block context using the block number
// and block time provided. This function is usually necessary to convert
// a `*types.Block` to be passed as a `contract.ConfigurationBlockContext`
// interface to [ApplyUpgrades].
func NewBlockContext(number *big.Int, timestamp uint64) *BlockContext {
	return &BlockContext{
		number:    number,
		timestamp: timestamp,
	}
}

func (bc *BlockContext) Number() *big.Int  { return bc.number }
func (bc *BlockContext) Timestamp() uint64 { return bc.timestamp }
