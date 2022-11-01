// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
)

type blockValidatorCamino struct {
	avalancheBlockValidator BlockValidator
}

func NewCaminoBlockValidator(extDataHashes map[common.Hash]common.Hash) BlockValidator {
	return &blockValidatorCamino{
		avalancheBlockValidator: NewBlockValidator(extDataHashes),
	}
}

func (bv blockValidatorCamino) SyntacticVerify(b *Block, rules params.Rules) error {
	// Use avalanche verification for old blocks
	if !rules.IsSunrisePhase0 {
		return bv.SyntacticVerify(b, rules)
	}

	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	// Skip verification of the genesis block since it
	// should already be marked as accepted
	if b.ethBlock.Hash() == b.vm.genesisHash {
		return nil
	}

	// Perform block and header sanity checks
	ethHeader := b.ethBlock.Header()
	if ethHeader.Number == nil || !ethHeader.Number.IsUint64() {
		return errInvalidBlock
	}
	if ethHeader.Difficulty == nil || !ethHeader.Difficulty.IsUint64() ||
		ethHeader.Difficulty.Uint64() != 1 {
		return fmt.Errorf("invalid difficulty: %d", ethHeader.Difficulty)
	}
	if ethHeader.Nonce.Uint64() != 0 {
		return fmt.Errorf(
			"expected nonce to be 0 but got %d: %w",
			ethHeader.Nonce.Uint64(), errInvalidNonce,
		)
	}
	if ethHeader.GasLimit != params.ApricotPhase1GasLimit {
		return fmt.Errorf(
			"expected gas limit to be %d in apricot phase 1 but got %d",
			params.ApricotPhase1GasLimit, ethHeader.GasLimit,
		)
	}
	if ethHeader.MixDigest != (common.Hash{}) {
		return fmt.Errorf("invalid mix digest: %v", ethHeader.MixDigest)
	}
	if hash := types.CalcExtDataHash(b.ethBlock.ExtData()); ethHeader.ExtDataHash != hash {
		return fmt.Errorf("extra data hash mismatch: have %x, want %x", ethHeader.ExtDataHash, hash)
	}
	if headerExtraDataSize := len(ethHeader.Extra); headerExtraDataSize != params.SunrisePhase0ExtraDataSize {
		return fmt.Errorf(
			"expected header ExtraData to be %d but got %d",
			params.SunrisePhase0ExtraDataSize, headerExtraDataSize,
		)
	}
	if ethHeader.BaseFee == nil {
		return errNilBaseFeeApricotPhase3
	}
	if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
		return fmt.Errorf("too large base fee: bitlen %d", bfLen)
	}
	if b.ethBlock.Version() != 0 {
		return fmt.Errorf("invalid version: %d", b.ethBlock.Version())
	}

	// Check that the tx hash in the header matches the body
	txsHash := types.DeriveSha(b.ethBlock.Transactions(), new(trie.Trie))
	if txsHash != ethHeader.TxHash {
		return fmt.Errorf("invalid txs hash %v does not match calculated txs hash %v", ethHeader.TxHash, txsHash)
	}
	// Check that the uncle hash in the header matches the body
	uncleHash := types.CalcUncleHash(b.ethBlock.Uncles())
	if uncleHash != ethHeader.UncleHash {
		return fmt.Errorf("invalid uncle hash %v does not match calculated uncle hash %v", ethHeader.UncleHash, uncleHash)
	}
	// Coinbase must be zero on C-Chain
	if b.ethBlock.Coinbase() != constants.BlackholeAddr {
		return fmt.Errorf("invalid coinbase %v does not match required blackhole address %v", ethHeader.Coinbase, constants.BlackholeAddr)
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	// Block must not be empty
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 && len(b.atomicTxs) == 0 {
		return errEmptyBlock
	}

	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}

	// Make sure ExtDataGasUsed is not nil and correct
	if ethHeader.ExtDataGasUsed == nil {
		return errNilExtDataGasUsedApricotPhase4
	}
	if ethHeader.ExtDataGasUsed.Cmp(params.AtomicGasLimit) == 1 {
		return fmt.Errorf("too large extDataGasUsed: %d", ethHeader.ExtDataGasUsed)
	}

	var totalGasUsed uint64
	for _, atomicTx := range b.atomicTxs {
		// We perform this check manually here to avoid the overhead of having to
		// reparse the atomicTx in `CalcExtDataGasUsed`.
		gasUsed, err := atomicTx.GasUsed(true)
		if err != nil {
			return err
		}
		totalGasUsed, err = safemath.Add64(totalGasUsed, gasUsed)
		if err != nil {
			return err
		}
	}

	switch {
	case ethHeader.ExtDataGasUsed.Cmp(new(big.Int).SetUint64(totalGasUsed)) != 0:
		return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", ethHeader.ExtDataGasUsed, totalGasUsed)

	// Make sure BlockGasCost is not nil
	// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
	case ethHeader.BlockGasCost == nil:
		return errNilBlockGasCostApricotPhase4
	case !ethHeader.BlockGasCost.IsUint64():
		return fmt.Errorf("too large blockGasCost: %d", ethHeader.BlockGasCost)
	}
	return nil
}
