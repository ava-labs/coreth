// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
)

var (
	apricotPhase0MinGasPrice = big.NewInt(params.LaunchMinGasPrice)
	apricotPhase1MinGasPrice = big.NewInt(params.ApricotPhase1MinGasPrice)
)

// newBlock returns a new Block wrapping the ethBlock type and implementing the snowman.Block interface
func (vm *VM) newBlock(ethBlock *types.Block) (*Block, error) {
	return &Block{
		id:        ids.ID(ethBlock.Hash()),
		ethBlock:  ethBlock,
		extension: vm.extensionConfig.BlockExtension,
		vm:        vm,
	}, nil
}

func (b *Block) SyntacticVerify(rules params.Rules) error {
	ethHeader := b.ethBlock.Header()

	// Perform block and header sanity checks
	if !ethHeader.Number.IsUint64() {
		return fmt.Errorf("invalid block number: %v", ethHeader.Number)
	}
	if !ethHeader.Difficulty.IsUint64() || ethHeader.Difficulty.Cmp(common.Big1) != 0 {
		return fmt.Errorf("invalid difficulty: %d", ethHeader.Difficulty)
	}
	if ethHeader.Nonce.Uint64() != 0 {
		return fmt.Errorf(
			"expected nonce to be 0 but got %d: %w",
			ethHeader.Nonce.Uint64(), errInvalidNonce,
		)
	}

	if ethHeader.MixDigest != (common.Hash{}) {
		return fmt.Errorf("invalid mix digest: %v", ethHeader.MixDigest)
	}

	// Enforce static gas limit after ApricotPhase1 (prior to ApricotPhase1 it's handled in processing).
	if rules.IsCortina {
		if ethHeader.GasLimit != params.CortinaGasLimit {
			return fmt.Errorf(
				"expected gas limit to be %d after cortina but got %d",
				params.CortinaGasLimit, ethHeader.GasLimit,
			)
		}
	} else if rules.IsApricotPhase1 {
		if ethHeader.GasLimit != params.ApricotPhase1GasLimit {
			return fmt.Errorf(
				"expected gas limit to be %d after apricot phase 1 but got %d",
				params.ApricotPhase1GasLimit, ethHeader.GasLimit,
			)
		}
	}

	// Check that the size of the header's Extra data field is correct for [rules].
	headerExtraDataSize := len(ethHeader.Extra)
	switch {
	case rules.IsDurango:
		if headerExtraDataSize < params.DynamicFeeExtraDataSize {
			return fmt.Errorf(
				"expected header ExtraData to be len >= %d but got %d",
				params.DynamicFeeExtraDataSize, len(ethHeader.Extra),
			)
		}
	case rules.IsApricotPhase3:
		if headerExtraDataSize != params.DynamicFeeExtraDataSize {
			return fmt.Errorf(
				"expected header ExtraData to be len %d but got %d",
				params.DynamicFeeExtraDataSize, headerExtraDataSize,
			)
		}
	case rules.IsApricotPhase1:
		if headerExtraDataSize != 0 {
			return fmt.Errorf(
				"expected header ExtraData to be 0 but got %d",
				headerExtraDataSize,
			)
		}
	default:
		if uint64(headerExtraDataSize) > params.MaximumExtraDataSize {
			return fmt.Errorf(
				"expected header ExtraData to be <= %d but got %d",
				params.MaximumExtraDataSize, headerExtraDataSize,
			)
		}
	}

	if b.ethBlock.Version() != 0 {
		return fmt.Errorf("invalid version: %d", b.ethBlock.Version())
	}

	// Check that the tx hash in the header matches the body
	txsHash := types.DeriveSha(b.ethBlock.Transactions(), trie.NewStackTrie(nil))
	if txsHash != ethHeader.TxHash {
		return fmt.Errorf("invalid txs hash %v does not match calculated txs hash %v", ethHeader.TxHash, txsHash)
	}
	// Check that the uncle hash in the header matches the body
	uncleHash := types.CalcUncleHash(b.ethBlock.Uncles())
	if uncleHash != ethHeader.UncleHash {
		return fmt.Errorf("invalid uncle hash %v does not match calculated uncle hash %v", ethHeader.UncleHash, uncleHash)
	}
	// Coinbase must match the BlackholeAddr on C-Chain
	if ethHeader.Coinbase != constants.BlackholeAddr {
		return fmt.Errorf("invalid coinbase %v does not match required blackhole address %v", ethHeader.Coinbase, constants.BlackholeAddr)
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}

	// Enforce minimum gas prices here prior to dynamic fees going into effect.
	switch {
	case !rules.IsApricotPhase1:
		// If we are in ApricotPhase0, enforce each transaction has a minimum gas price of at least the LaunchMinGasPrice
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(apricotPhase0MinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), params.LaunchMinGasPrice)
			}
		}
	case !rules.IsApricotPhase3:
		// If we are prior to ApricotPhase3, enforce each transaction has a minimum gas price of at least the ApricotPhase1MinGasPrice
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(apricotPhase1MinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), params.ApricotPhase1MinGasPrice)
			}
		}
	}

	// Ensure BaseFee is non-nil as of ApricotPhase3.
	if rules.IsApricotPhase3 {
		if ethHeader.BaseFee == nil {
			return errNilBaseFeeApricotPhase3
		}
		// TODO: this should be removed as 256 is the maximum possible bit length of a big int
		if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
			return fmt.Errorf("too large base fee: bitlen %d", bfLen)
		}
	}

	if rules.IsApricotPhase4 {
		switch {
		// Make sure BlockGasCost is not nil
		// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
		case ethHeader.BlockGasCost == nil:
			return errNilBlockGasCostApricotPhase4
		case !ethHeader.BlockGasCost.IsUint64():
			return fmt.Errorf("too large blockGasCost: %d", ethHeader.BlockGasCost)
		}
	}

	// Verify the existence / non-existence of excessBlobGas
	cancun := rules.IsCancun
	if !cancun && ethHeader.ExcessBlobGas != nil {
		return fmt.Errorf("invalid excessBlobGas: have %d, expected nil", *ethHeader.ExcessBlobGas)
	}
	if !cancun && ethHeader.BlobGasUsed != nil {
		return fmt.Errorf("invalid blobGasUsed: have %d, expected nil", *ethHeader.BlobGasUsed)
	}
	if cancun && ethHeader.ExcessBlobGas == nil {
		return errors.New("header is missing excessBlobGas")
	}
	if cancun && ethHeader.BlobGasUsed == nil {
		return errors.New("header is missing blobGasUsed")
	}
	if !cancun && ethHeader.ParentBeaconRoot != nil {
		return fmt.Errorf("invalid parentBeaconRoot: have %x, expected nil", *ethHeader.ParentBeaconRoot)
	}

	if cancun {
		switch {
		case ethHeader.ParentBeaconRoot == nil:
			return errors.New("header is missing parentBeaconRoot")
		case *ethHeader.ParentBeaconRoot != (common.Hash{}):
			return fmt.Errorf("invalid parentBeaconRoot: have %x, expected empty hash", ethHeader.ParentBeaconRoot)
		}
		if ethHeader.BlobGasUsed == nil {
			return fmt.Errorf("blob gas used must not be nil in Cancun")
		} else if *ethHeader.BlobGasUsed > 0 {
			return fmt.Errorf("blobs not enabled on avalanche networks: used %d blob gas, expected 0", *ethHeader.BlobGasUsed)
		}
	}

	if b.extension != nil {
		return b.extension.SyntacticVerify(b, rules)
	}
	return nil
}

func (b *Block) SemanticVerify() error {
	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}

	if b.extension != nil {
		return b.extension.SemanticVerify(b)
	}
	return nil
}
