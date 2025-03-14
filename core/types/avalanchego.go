// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// TODO(arr4n): this is a temporary workaround because of the circular
// dependency between the coreth and avalanchego repos. The latter depends on
// these types via coreth instead of via libevm so there's a chicken-and-egg
// problem with refactoring both repos.

type (
	// Deprecated: use the RHS type directly.
	DynamicFeeTx = ethtypes.DynamicFeeTx
	// Deprecated: use the RHS type directly.
	Receipt = ethtypes.Receipt
	// Deprecated: use the RHS type directly.
	Transaction = ethtypes.Transaction
)

// Deprecated: use [ethtypes.NewEIP155Signer] directly.
func NewEIP155Signer(chainID *big.Int) ethtypes.EIP155Signer {
	return ethtypes.NewEIP155Signer(chainID)
}

// Deprecated: use [ethtypes.NewLondonSigner] directly.
func NewLondonSigner(chainID *big.Int) ethtypes.Signer {
	return ethtypes.NewLondonSigner(chainID)
}

// Deprecated: use [ethtypes.NewTransaction] directly.
func NewTransaction(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *ethtypes.Transaction {
	return ethtypes.NewTransaction(nonce, to, amount, gasLimit, gasPrice, data)
}

// Deprecated: use [ethtypes.NewTx] directly.
func NewTx(inner ethtypes.TxData) *ethtypes.Transaction {
	return ethtypes.NewTx(inner)
}

// Deprecated: use [ethtypes.SignTx] directly.
func SignTx(tx *ethtypes.Transaction, s ethtypes.Signer, prv *ecdsa.PrivateKey) (*ethtypes.Transaction, error) {
	return ethtypes.SignTx(tx, s, prv)
}

const (
	// Deprecated: use the RHS value directly.
	ReceiptStatusSuccessful = ethtypes.ReceiptStatusSuccessful
	// Deprecated: use the RHS value directly.
	ReceiptStatusFailed = ethtypes.ReceiptStatusFailed
)
