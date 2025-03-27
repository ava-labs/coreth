// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/libevm/core/types"
)

var (
	ap0MinGasPrice = big.NewInt(ap0.MinGasPrice)
	ap1MinGasPrice = big.NewInt(ap1.MinGasPrice)
)

// syntacticVerify verifies that a *Block is well-formed.
// TODO: This is kept here to reduce the diff, move this function to block.go
func (b *Block) syntacticVerify() error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	// Skip verification of the genesis block since it should already be marked as accepted.
	if b.ethBlock.Hash() == b.vm.genesisHash {
		return nil
	}

	ethHeader := b.ethBlock.Header()
	rules := b.vm.chainConfig.Rules(ethHeader.Number, params.IsMergeTODO, ethHeader.Time)
	rulesExtra := params.GetRulesExtra(rules)
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

	// Verify the extra data is well-formed.
	if err := header.VerifyExtra(rulesExtra.AvalancheRules, ethHeader.Extra); err != nil {
		return err
	}

	if version := customtypes.BlockVersion(b.ethBlock); version != 0 {
		return fmt.Errorf("invalid version: %d", version)
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
	case !rulesExtra.IsApricotPhase1:
		// If we are in ApricotPhase0, enforce each transaction has a minimum gas price of at least the LaunchMinGasPrice
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(ap0MinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), ap0.MinGasPrice)
			}
		}
	case !rulesExtra.IsApricotPhase3:
		// If we are prior to ApricotPhase3, enforce each transaction has a minimum gas price of at least the ApricotPhase1MinGasPrice
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(ap1MinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), ap1.MinGasPrice)
			}
		}
	}

	// Ensure BaseFee is non-nil as of ApricotPhase3.
	if rulesExtra.IsApricotPhase3 {
		if ethHeader.BaseFee == nil {
			return errNilBaseFeeApricotPhase3
		}
		// TODO: this should be removed as 256 is the maximum possible bit length of a big int
		if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
			return fmt.Errorf("too large base fee: bitlen %d", bfLen)
		}
	}

	headerExtra := customtypes.GetHeaderExtra(ethHeader)
	if rulesExtra.IsApricotPhase4 {
		switch {
		// Make sure BlockGasCost is not nil
		// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
		case headerExtra.BlockGasCost == nil:
			return errNilBlockGasCostApricotPhase4
		case !headerExtra.BlockGasCost.IsUint64():
			return fmt.Errorf("too large blockGasCost: %d", headerExtra.BlockGasCost)
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
		if err := b.extension.SyntacticVerify(b, rules); err != nil {
			return err
		}
	}
	return nil
}
