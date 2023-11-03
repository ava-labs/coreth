// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm"
)

type IntegrationFixture interface {
	IssueTxs(txs []*types.Transaction)
	IssueAtomicTxs(atomicTxs []*evm.Tx)

	BuildAndAccept()

	Teardown()
	// ConfirmTxs(txs []*types.Transaction)
	// ConfirmAtomicTxs(atomicTxs []*evm.Tx)

	// Define index getter functions
	// GetTxReceipt(txHash common.Hash) *types.Receipt
}
