// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
)

var _ atomic.Visitor = (*semanticVerifier)(nil)

var (
	errAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	errConflictingAtomicInputs    = errors.New("invalid block due to conflicting atomic inputs")
	errRejectedParent             = errors.New("rejected parent")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
)

type BlockFetcher interface {
	LastAcceptedVMBlock() extension.VMBlock
	GetVMBlock(context.Context, ids.ID) (extension.VMBlock, error)
}

type verifierBackend struct {
	ctx          *snow.Context
	fx           fx.Fx
	rules        extras.Rules
	bootstrapped bool
	blockFetcher BlockFetcher
	secpCache    *secp256k1.RecoverCache
}

// semanticVerifier is a visitor that checks the semantic validity of atomic transactions.
type semanticVerifier struct {
	backend *verifierBackend
	tx      *atomic.Tx
	parent  extension.VMBlock
	baseFee *big.Int
}

// ImportTx verifies this transaction is valid.
func (s *semanticVerifier) ImportTx(utx *atomic.UnsignedImportTx) error {
	backend := s.backend
	ctx := backend.ctx
	rules := backend.rules
	stx := s.tx
	if err := utx.Verify(ctx, rules); err != nil {
		return err
	}

	// Check the transaction consumes and produces the right amounts
	fc := avax.NewFlowChecker()
	switch {
	// Apply dynamic fees to import transactions as of Apricot Phase 3
	case rules.IsApricotPhase3:
		gasUsed, err := stx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return err
		}
		txFee, err := atomic.CalculateDynamicFee(gasUsed, s.baseFee)
		if err != nil {
			return err
		}
		fc.Produce(ctx.AVAXAssetID, txFee)

	// Apply fees to import transactions as of Apricot Phase 2
	case rules.IsApricotPhase2:
		fc.Produce(ctx.AVAXAssetID, ap0.AtomicTxFee)
	}
	for _, out := range utx.Outs {
		fc.Produce(out.AssetID, out.Amount)
	}
	for _, in := range utx.ImportedInputs {
		fc.Consume(in.AssetID(), in.Input().Amount())
	}

	if err := fc.Verify(); err != nil {
		return fmt.Errorf("import tx flow check failed due to: %w", err)
	}

	if len(stx.Creds) != len(utx.ImportedInputs) {
		return fmt.Errorf("import tx contained mismatched number of inputs/credentials (%d vs. %d)", len(utx.ImportedInputs), len(stx.Creds))
	}

	if !backend.bootstrapped {
		// Allow for force committing during bootstrapping
		return nil
	}

	utxoIDs := make([][]byte, len(utx.ImportedInputs))
	for i, in := range utx.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}
	// allUTXOBytes is guaranteed to be the same length as utxoIDs
	allUTXOBytes, err := ctx.SharedMemory.Get(utx.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("failed to fetch import UTXOs from %s due to: %w", utx.SourceChain, err)
	}

	for i, in := range utx.ImportedInputs {
		utxoBytes := allUTXOBytes[i]

		utxo := &avax.UTXO{}
		if _, err := atomic.Codec.Unmarshal(utxoBytes, utxo); err != nil {
			return fmt.Errorf("failed to unmarshal UTXO: %w", err)
		}

		cred := stx.Creds[i]

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if utxoAssetID != inAssetID {
			return errAssetIDMismatch
		}

		if err := backend.fx.VerifyTransfer(utx, in.In, cred, utxo.Out); err != nil {
			return fmt.Errorf("import tx transfer failed verification: %w", err)
		}
	}

	return conflicts(backend, utx.InputUTXOs(), s.parent)
}

// conflicts returns an error if [inputs] conflicts with any of the atomic inputs contained in [ancestor]
// or any of its ancestor blocks going back to the last accepted block in its ancestry. If [ancestor] is
// accepted, then nil will be returned immediately.
// If the ancestry of [ancestor] cannot be fetched, then [errRejectedParent] may be returned.
func conflicts(backend *verifierBackend, inputs set.Set[ids.ID], ancestor extension.VMBlock) error {
	lastAcceptedBlock := backend.blockFetcher.LastAcceptedVMBlock()
	lastAcceptedHeight := lastAcceptedBlock.Height()
	for ancestor.Height() > lastAcceptedHeight {
		blockExtension, ok := ancestor.GetBlockExtension().(*blockExtension)
		if !ok {
			return fmt.Errorf("expected block extension to be of type *blockExtension but got %T", ancestor.GetBlockExtension())
		}
		// If any of the atomic transactions in the ancestor conflict with [inputs]
		// return an error.
		for _, atomicTx := range blockExtension.atomicTxs {
			if inputs.Overlaps(atomicTx.InputUTXOs()) {
				return errConflictingAtomicInputs
			}
		}

		// Move up the chain.
		nextAncestorID := ancestor.Parent()
		// If the ancestor is unknown, then the parent failed
		// verification when it was called.
		// If the ancestor is rejected, then this block shouldn't be
		// inserted into the canonical chain because the parent is
		// will be missing.
		// If the ancestor is processing, then the block may have
		// been verified.
		nextAncestor, err := backend.blockFetcher.GetVMBlock(context.TODO(), nextAncestorID)
		if err != nil {
			return errRejectedParent
		}
		ancestor = nextAncestor
	}

	return nil
}

// ExportTx verifies this transaction is valid.
func (s *semanticVerifier) ExportTx(utx *atomic.UnsignedExportTx) error {
	ctx := s.backend.ctx
	rules := s.backend.rules
	stx := s.tx
	if err := utx.Verify(ctx, rules); err != nil {
		return err
	}

	// Check the transaction consumes and produces the right amounts
	fc := avax.NewFlowChecker()
	switch {
	// Apply dynamic fees to export transactions as of Apricot Phase 3
	case rules.IsApricotPhase3:
		gasUsed, err := stx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return err
		}
		txFee, err := atomic.CalculateDynamicFee(gasUsed, s.baseFee)
		if err != nil {
			return err
		}
		fc.Produce(ctx.AVAXAssetID, txFee)
	// Apply fees to export transactions before Apricot Phase 3
	default:
		fc.Produce(ctx.AVAXAssetID, ap0.AtomicTxFee)
	}
	for _, out := range utx.ExportedOutputs {
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	for _, in := range utx.Ins {
		fc.Consume(in.AssetID, in.Amount)
	}

	if err := fc.Verify(); err != nil {
		return fmt.Errorf("export tx flow check failed due to: %w", err)
	}

	if len(utx.Ins) != len(stx.Creds) {
		return fmt.Errorf("export tx contained mismatched number of inputs/credentials (%d vs. %d)", len(utx.Ins), len(stx.Creds))
	}

	for i, input := range utx.Ins {
		cred, ok := stx.Creds[i].(*secp256k1fx.Credential)
		if !ok {
			return fmt.Errorf("expected *secp256k1fx.Credential but got %T", cred)
		}
		if err := cred.Verify(); err != nil {
			return err
		}

		if len(cred.Sigs) != 1 {
			return fmt.Errorf("expected one signature for EVM Input Credential, but found: %d", len(cred.Sigs))
		}
		pubKey, err := s.backend.secpCache.RecoverPublicKey(utx.Bytes(), cred.Sigs[0][:])
		if err != nil {
			return err
		}
		if input.Address != pubKey.EthAddress() {
			return errPublicKeySignatureMismatch
		}
	}

	return nil
}
