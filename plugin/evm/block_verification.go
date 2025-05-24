// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
)

var (
	errUnclesUnsupported = errors.New("uncles unsupported")
	errInvalidUncleHash  = errors.New("invalid uncle hash")
	errInvalidCoinbase   = errors.New("invalid coinbase")
	errInvalidTxHash     = errors.New("invalid tx hash")
	errInvalidDifficulty = errors.New("invalid difficulty")
	errInvalidNumber     = errors.New("invalid number")
)

type blockValidator struct {
	extDataHashes map[common.Hash]common.Hash
}

func newBlockValidator(extDataHashes map[common.Hash]common.Hash) *blockValidator {
	return &blockValidator{
		extDataHashes: extDataHashes,
	}
}

func verifyBlockStandalone(
	extDataHashes map[common.Hash]common.Hash,
	clock *mockable.Clock,
	config *params.ChainConfig,
	block *types.Block,
	atomicTxs []*atomic.Tx,
) error {
	var (
		ethHeader        = block.Header()
		txs              = block.Transactions()
		txsHash          = types.DeriveSha(txs, trie.NewStackTrie(nil))
		maxBlockTime     = clock.Unix() + maxFutureBlockTime
		rules            = config.Rules(ethHeader.Number, params.IsMergeTODO, ethHeader.Time)
		rulesExtra       = params.GetRulesExtra(rules)
		errExtraValidity = header.VerifyExtra(rulesExtra.AvalancheRules, ethHeader.Extra)
		version          = customtypes.BlockVersion(block)
	)
	switch {
	case len(block.Uncles()) != 0: // Block must not have any uncles
		return errUnclesUnsupported
	case ethHeader.UncleHash != types.EmptyUncleHash:
		return fmt.Errorf("%w: %x", errInvalidUncleHash, ethHeader.UncleHash)
	case ethHeader.Coinbase != constants.BlackholeAddr:
		return fmt.Errorf("%w: %x", errInvalidCoinbase, ethHeader.Coinbase)
	case txsHash != ethHeader.TxHash:
		return fmt.Errorf("%w: %x does not match expected %x", errInvalidTxHash, ethHeader.TxHash, txsHash)
	case !utils.BigEqualUint64(ethHeader.Difficulty, 1):
		return fmt.Errorf("%w: %d", errInvalidDifficulty, ethHeader.Difficulty)
	case ethHeader.Number == nil || !ethHeader.Number.IsUint64():
		return fmt.Errorf("%w: %v", errInvalidNumber, ethHeader.Number)
	case ethHeader.Time > maxBlockTime: // Make sure the block isn't too far in the future
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", ethHeader.Time, maxBlockTime)
	case errExtraValidity != nil:
		return errExtraValidity
	case ethHeader.MixDigest != (common.Hash{}):
		return fmt.Errorf("invalid mix digest: %v", ethHeader.MixDigest)
	case ethHeader.Nonce.Uint64() != 0:
		return fmt.Errorf("%w: %d", errInvalidNonce, ethHeader.Nonce.Uint64())
	case ethHeader.WithdrawalsHash != nil:
		return fmt.Errorf("unexpected withdrawalsHash: %x", ethHeader.WithdrawalsHash)
	case version != 0:
		return fmt.Errorf("invalid version: %d", version)
	case len(txs) == 0 && len(atomicTxs) == 0: // Block must not be empty
		return errEmptyBlock
	}

	for i, tx := range txs {
		if len(tx.BlobHashes()) != 0 {
			return fmt.Errorf("unexpected blobs in transaction at index %d", i)
		}
	}

	// Verify the ExtDataHash field
	headerExtra := customtypes.GetHeaderExtra(ethHeader)
	if rulesExtra.IsApricotPhase1 {
		extraData := customtypes.BlockExtData(block)
		hash := customtypes.CalcExtDataHash(extraData)
		if headerExtra.ExtDataHash != hash {
			return fmt.Errorf("extra data hash mismatch: have %x, want %x", headerExtra.ExtDataHash, hash)
		}
	} else {
		var (
			blockHash = block.Hash()
			extData   = customtypes.BlockExtData(block)
		)
		expectedExtDataHash, ok := extDataHashes[blockHash]
		if len(extData) == 0 {
			// If there is no extra data, check that there is no extra data in
			// the hash map either to ensure we do not have a block that is
			// unexpectedly missing extra data.
			if ok {
				return fmt.Errorf("found block with unexpected missing extra data (%s, %d), expected extra data hash: %s", blockHash, block.NumberU64(), expectedExtDataHash)
			}
		} else {
			// If there is extra data, check to make sure that the extra data
			// hash matches the expected extra data hash for this block.
			extDataHash := customtypes.CalcExtDataHash(extData)
			if extDataHash != expectedExtDataHash {
				return fmt.Errorf("extra data hash in block (%s, %d): %s, did not match the expected extra data hash: %s", blockHash, block.NumberU64(), extDataHash, expectedExtDataHash)
			}
		}
	}

	if rulesExtra.IsApricotPhase3 {
		if ethHeader.BaseFee == nil {
			return errNilBaseFeeApricotPhase3
		}
		if bitLen := ethHeader.BaseFee.BitLen(); bitLen > 256 {
			return fmt.Errorf("baseFee too large: bitLen %d", bitLen)
		}
	}

	// If we are in ApricotPhase4, ensure that ExtDataGasUsed is populated
	// correctly.
	if rulesExtra.IsApricotPhase4 {
		var totalGasUsed uint64
		for _, atomicTx := range atomicTxs {
			// We perform this check manually here to avoid the overhead of
			// having to reparse the atomicTx in `CalcExtDataGasUsed`.
			fixedFee := rulesExtra.IsApricotPhase5 // Charge the atomic tx fixed fee as of ApricotPhase5
			gasUsed, err := atomicTx.GasUsed(fixedFee)
			if err != nil {
				return err
			}
			totalGasUsed, err = math.Add(totalGasUsed, gasUsed)
			if err != nil {
				return err
			}
		}

		switch {
		case !utils.BigEqualUint64(headerExtra.ExtDataGasUsed, totalGasUsed):
			return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", headerExtra.ExtDataGasUsed, totalGasUsed)
		case headerExtra.BlockGasCost == nil:
			return errNilBlockGasCostApricotPhase4
		case !headerExtra.BlockGasCost.IsUint64():
			return fmt.Errorf("too large blockGasCost: %d", headerExtra.BlockGasCost)
		}
	}

	if rules.IsCancun {
		switch {
		case !utils.PointerEqualsValue(ethHeader.BlobGasUsed, 0):
			return fmt.Errorf("invalid BlobGasUsed: expected 0 got %v -> %d", ethHeader.BlobGasUsed, ethHeader.BlobGasUsed)
		case !utils.PointerEqualsValue(ethHeader.ExcessBlobGas, 0):
			return fmt.Errorf("invalid ExcessBlobGas: expected 0 got %v -> %d", ethHeader.ExcessBlobGas, ethHeader.ExcessBlobGas)
		case !utils.PointerEqualsValue(ethHeader.ParentBeaconRoot, common.Hash{}):
			return fmt.Errorf("invalid ParentBeaconRoot: expected empty hash got %x", ethHeader.ParentBeaconRoot)
		}
	}
	return nil
}
