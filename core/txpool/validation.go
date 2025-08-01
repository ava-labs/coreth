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
// Copyright 2023 The go-ethereum Authors
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

package txpool

import (
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto/kzg4844"
	"github.com/ava-labs/libevm/log"
)

var (
	// blobTxMinBlobGasPrice is the big.Int version of the configured protocol
	// parameter to avoid constucting a new big integer for every transaction.
	blobTxMinBlobGasPrice = big.NewInt(params.BlobTxMinBlobGasprice)
)

// ValidationOptions define certain differences between transaction validation
// across the different pools without having to duplicate those checks.
type ValidationOptions struct {
	Config *params.ChainConfig // Chain configuration to selectively validate based on current fork rules

	Accept  uint8    // Bitmap of transaction types that should be accepted for the calling pool
	MaxSize uint64   // Maximum size of a transaction that the caller can meaningfully handle
	MinTip  *big.Int // Minimum gas tip needed to allow a transaction into the caller pool
}

// ValidateTransaction is a helper method to check whether a transaction is valid
// according to the consensus rules, but does not check state-dependent validation
// (balance, nonce, etc).
//
// This check is public to allow different transaction pools to check the basic
// rules without duplicating code and running the risk of missed updates.
func ValidateTransaction(tx *types.Transaction, head *types.Header, signer types.Signer, opts *ValidationOptions) error {
	// Ensure transactions not implemented by the calling pool are rejected
	if opts.Accept&(1<<tx.Type()) == 0 {
		return fmt.Errorf("%w: tx type %v not supported by this pool", core.ErrTxTypeNotSupported, tx.Type())
	}
	// Before performing any expensive validations, sanity check that the tx is
	// smaller than the maximum limit the pool can meaningfully handle
	if tx.Size() > opts.MaxSize {
		return fmt.Errorf("%w: transaction size %v, limit %v", ErrOversizedData, tx.Size(), opts.MaxSize)
	}
	// Ensure only transactions that have been enabled are accepted
	if !opts.Config.IsBerlin(head.Number) && tx.Type() != types.LegacyTxType {
		return fmt.Errorf("%w: type %d rejected, pool not yet in Berlin", core.ErrTxTypeNotSupported, tx.Type())
	}
	if !opts.Config.IsLondon(head.Number) && tx.Type() == types.DynamicFeeTxType {
		return fmt.Errorf("%w: type %d rejected, pool not yet in London", core.ErrTxTypeNotSupported, tx.Type())
	}
	if !opts.Config.IsCancun(head.Number, head.Time) && tx.Type() == types.BlobTxType {
		return fmt.Errorf("%w: type %d rejected, pool not yet in Cancun", core.ErrTxTypeNotSupported, tx.Type())
	}
	// Check whether the init code size has been exceeded
	if opts.Config.IsShanghai(head.Number, head.Time) && tx.To() == nil && len(tx.Data()) > params.MaxInitCodeSize {
		return fmt.Errorf("%w: code size %v, limit %v", vm.ErrMaxInitCodeSizeExceeded, len(tx.Data()), params.MaxInitCodeSize)
	}
	// Transactions can't be negative. This may never happen using RLP decoded
	// transactions but may occur for transactions created using the RPC.
	if tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}
	// Ensure the transaction doesn't exceed the current block limit gas
	if txGas := tx.Gas(); head.GasLimit < txGas {
		return fmt.Errorf(
			"%w: tx gas (%d) > current max gas (%d)",
			ErrGasLimit,
			txGas,
			head.GasLimit,
		)
	}
	// Sanity check for extremely large numbers (supported by RLP or RPC)
	if tx.GasFeeCap().BitLen() > 256 {
		return core.ErrFeeCapVeryHigh
	}
	if tx.GasTipCap().BitLen() > 256 {
		return core.ErrTipVeryHigh
	}
	// Ensure gasFeeCap is greater than or equal to gasTipCap
	if tx.GasFeeCapIntCmp(tx.GasTipCap()) < 0 {
		return core.ErrTipAboveFeeCap
	}
	// Make sure the transaction is signed properly
	from, err := types.Sender(signer, tx)
	if err != nil {
		return ErrInvalidSender
	}
	// Ensure the transaction has more gas than the bare minimum needed to cover
	// the transaction metadata
	intrGas, err := core.IntrinsicGas(tx.Data(), tx.AccessList(), tx.To() == nil, opts.Config.Rules(head.Number, params.IsMergeTODO, head.Time))
	if err != nil {
		return err
	}
	if txGas := tx.Gas(); txGas < intrGas {
		return fmt.Errorf("%w: address %v tx gas (%v), minimum needed %v", core.ErrIntrinsicGas, from.Hex(), txGas, intrGas)
	}
	// Ensure the gasprice is high enough to cover the requirement of the calling pool
	if tx.GasTipCapIntCmp(opts.MinTip) < 0 {
		return fmt.Errorf("%w: gas tip cap %v, minimum needed %v", ErrUnderpriced, tx.GasTipCap(), opts.MinTip)
	}
	if tx.Type() == types.BlobTxType {
		// Ensure the blob fee cap satisfies the minimum blob gas price
		if tx.BlobGasFeeCapIntCmp(blobTxMinBlobGasPrice) < 0 {
			return fmt.Errorf("%w: blob fee cap %v, minimum needed %v", ErrUnderpriced, tx.BlobGasFeeCap(), blobTxMinBlobGasPrice)
		}
		sidecar := tx.BlobTxSidecar()
		if sidecar == nil {
			return fmt.Errorf("missing sidecar in blob transaction")
		}
		// Ensure the number of items in the blob transaction and various side
		// data match up before doing any expensive validations
		hashes := tx.BlobHashes()
		if len(hashes) == 0 {
			return fmt.Errorf("blobless blob transaction")
		}
		if len(hashes) > params.MaxBlobGasPerBlock/params.BlobTxBlobGasPerBlob {
			return fmt.Errorf("too many blobs in transaction: have %d, permitted %d", len(hashes), params.MaxBlobGasPerBlock/params.BlobTxBlobGasPerBlob)
		}
		// Ensure commitments, proofs and hashes are valid
		if err := validateBlobSidecar(hashes, sidecar); err != nil {
			return err
		}
	}
	return nil
}

func validateBlobSidecar(hashes []common.Hash, sidecar *types.BlobTxSidecar) error {
	if len(sidecar.Blobs) != len(hashes) {
		return fmt.Errorf("invalid number of %d blobs compared to %d blob hashes", len(sidecar.Blobs), len(hashes))
	}
	if len(sidecar.Commitments) != len(hashes) {
		return fmt.Errorf("invalid number of %d blob commitments compared to %d blob hashes", len(sidecar.Commitments), len(hashes))
	}
	if len(sidecar.Proofs) != len(hashes) {
		return fmt.Errorf("invalid number of %d blob proofs compared to %d blob hashes", len(sidecar.Proofs), len(hashes))
	}
	// Blob quantities match up, validate that the provers match with the
	// transaction hash before getting to the cryptography
	hasher := sha256.New()
	for i, vhash := range hashes {
		computed := kzg4844.CalcBlobHashV1(hasher, &sidecar.Commitments[i])
		if vhash != computed {
			return fmt.Errorf("blob %d: computed hash %#x mismatches transaction one %#x", i, computed, vhash)
		}
	}
	// Blob commitments match with the hashes in the transaction, verify the
	// blobs themselves via KZG
	for i := range sidecar.Blobs {
		if err := kzg4844.VerifyBlobProof(&sidecar.Blobs[i], sidecar.Commitments[i], sidecar.Proofs[i]); err != nil {
			return fmt.Errorf("invalid blob %d: %v", i, err)
		}
	}
	return nil
}

// ValidationOptionsWithState define certain differences between stateful transaction
// validation across the different pools without having to duplicate those checks.
type ValidationOptionsWithState struct {
	State *state.StateDB // State database to check nonces and balances against

	// FirstNonceGap is an optional callback to retrieve the first nonce gap in
	// the list of pooled transactions of a specific account. If this method is
	// set, nonce gaps will be checked and forbidden. If this method is not set,
	// nonce gaps will be ignored and permitted.
	FirstNonceGap func(addr common.Address) uint64

	// UsedAndLeftSlots is a mandatory callback to retrieve the number of tx slots
	// used and the number still permitted for an account. New transactions will
	// be rejected once the number of remaining slots reaches zero.
	UsedAndLeftSlots func(addr common.Address) (int, int)

	// ExistingExpenditure is a mandatory callback to retrieve the cumulative
	// cost of the already pooled transactions to check for overdrafts.
	ExistingExpenditure func(addr common.Address) *big.Int

	// ExistingCost is a mandatory callback to retrieve an already pooled
	// transaction's cost with the given nonce to check for overdrafts.
	ExistingCost func(addr common.Address, nonce uint64) *big.Int

	Rules      params.Rules
	MinimumFee *big.Int
}

// ValidateTransactionWithState is a helper method to check whether a transaction
// is valid according to the pool's internal state checks (balance, nonce, gaps).
//
// This check is public to allow different transaction pools to check the stateful
// rules without duplicating code and running the risk of missed updates.
func ValidateTransactionWithState(tx *types.Transaction, signer types.Signer, opts *ValidationOptionsWithState) error {
	// Ensure the transaction adheres to nonce ordering
	from, err := signer.Sender(tx) // already validated (and cached), but cleaner to check
	if err != nil {
		log.Error("Transaction sender recovery failed", "err", err)
		return err
	}

	// Drop the transaction if the gas fee cap is below the pool's minimum fee
	if opts.MinimumFee != nil && tx.GasFeeCapIntCmp(opts.MinimumFee) < 0 {
		return fmt.Errorf("%w: address %s have gas fee cap (%d) < pool minimum fee cap (%d)", ErrUnderpriced, from.Hex(), tx.GasFeeCap(), opts.MinimumFee)
	}

	next := opts.State.GetNonce(from)
	if next > tx.Nonce() {
		return fmt.Errorf("%w: next nonce %v, tx nonce %v", core.ErrNonceTooLow, next, tx.Nonce())
	}
	// Ensure the transaction doesn't produce a nonce gap in pools that do not
	// support arbitrary orderings
	if opts.FirstNonceGap != nil {
		if gap := opts.FirstNonceGap(from); gap < tx.Nonce() {
			return fmt.Errorf("%w: tx nonce %v, gapped nonce %v", core.ErrNonceTooHigh, tx.Nonce(), gap)
		}
	}
	// Ensure the transactor has enough funds to cover the transaction costs
	var (
		balance = opts.State.GetBalance(from).ToBig()
		cost    = tx.Cost()
	)
	if balance.Cmp(cost) < 0 {
		return fmt.Errorf("%w: balance %v, tx cost %v, overshot %v", core.ErrInsufficientFunds, balance, cost, new(big.Int).Sub(cost, balance))
	}
	// Ensure the transactor has enough funds to cover for replacements or nonce
	// expansions without overdrafts
	spent := opts.ExistingExpenditure(from)
	if prev := opts.ExistingCost(from, tx.Nonce()); prev != nil {
		bump := new(big.Int).Sub(cost, prev)
		need := new(big.Int).Add(spent, bump)
		if balance.Cmp(need) < 0 {
			return fmt.Errorf("%w: balance %v, queued cost %v, tx bumped %v, overshot %v", core.ErrInsufficientFunds, balance, spent, bump, new(big.Int).Sub(need, balance))
		}
	} else {
		need := new(big.Int).Add(spent, cost)
		if balance.Cmp(need) < 0 {
			return fmt.Errorf("%w: balance %v, queued cost %v, tx cost %v, overshot %v", core.ErrInsufficientFunds, balance, spent, cost, new(big.Int).Sub(need, balance))
		}
		// Transaction takes a new nonce value out of the pool. Ensure it doesn't
		// overflow the number of permitted transactions from a single account
		// (i.e. max cancellable via out-of-bound transaction).
		if used, left := opts.UsedAndLeftSlots(from); left <= 0 {
			return fmt.Errorf("%w: pooled %d txs", ErrAccountLimitExceeded, used)
		}
	}

	return nil
}
