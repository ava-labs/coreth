// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/database"
	safemath "github.com/ava-labs/avalanchego/utils/math"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/extension"
)

var _ extension.BlockManagerExtension = (*blockExtension)(nil)

var (
	errNilExtDataGasUsedApricotPhase4 = errors.New("nil extDataGasUsed is invalid after apricotPhase4")
	errNilEthBlock                    = errors.New("nil ethBlock")
	errMissingUTXOs                   = errors.New("missing UTXOs")
	errEmptyBlock                     = errors.New("empty block")
)

type blockExtension struct {
	extDataHashes map[common.Hash]common.Hash
	vm            *VM
}

// newBlockExtension returns a new block extension.
func newBlockExtension(
	extDataHashes map[common.Hash]common.Hash,
	vm *VM,
) *blockExtension {
	return &blockExtension{
		extDataHashes: extDataHashes,
		// Note: we need VM here to access the atomic backend that
		// could be initialized later in the VM.
		vm: vm,
	}
}

// SyntacticVerify checks the syntactic validity of the block. This is called the wrapper
// block manager's SyntacticVerify method.
func (be *blockExtension) SyntacticVerify(b extension.VMBlock, rules params.Rules) error {
	ethBlock := b.GetEthBlock()
	// should not happen
	if ethBlock == nil {
		return errNilEthBlock
	}
	ethHeader := ethBlock.Header()
	blockHash := ethBlock.Hash()

	if !rules.IsApricotPhase1 {
		if be.extDataHashes != nil {
			extData := ethBlock.ExtData()
			extDataHash := types.CalcExtDataHash(extData)
			// If there is no extra data, check that there is no extra data in the hash map either to ensure we do not
			// have a block that is unexpectedly missing extra data.
			expectedExtDataHash, ok := be.extDataHashes[blockHash]
			if len(extData) == 0 {
				if ok {
					return fmt.Errorf("found block with unexpected missing extra data (%s, %d), expected extra data hash: %s", blockHash, b.Height(), expectedExtDataHash)
				}
			} else {
				// If there is extra data, check to make sure that the extra data hash matches the expected extra data hash for this
				// block
				if extDataHash != expectedExtDataHash {
					return fmt.Errorf("extra data hash in block (%s, %d): %s, did not match the expected extra data hash: %s", blockHash, b.Height(), extDataHash, expectedExtDataHash)
				}
			}
		}
	}

	// Verify the ExtDataHash field
	if rules.IsApricotPhase1 {
		if hash := types.CalcExtDataHash(ethBlock.ExtData()); ethHeader.ExtDataHash != hash {
			return fmt.Errorf("extra data hash mismatch: have %x, want %x", ethHeader.ExtDataHash, hash)
		}
	} else {
		if ethHeader.ExtDataHash != (common.Hash{}) {
			return fmt.Errorf(
				"expected ExtDataHash to be empty but got %x",
				ethHeader.ExtDataHash,
			)
		}
	}

	// Block must not be empty
	txs := ethBlock.Transactions()
	atomicTxs, err := extractAtomicTxsFromBlock(b, be.vm.Ethereum().BlockChain().Config())
	if err != nil {
		return err
	}
	if len(txs) == 0 && len(atomicTxs) == 0 {
		return errEmptyBlock
	}

	// If we are in ApricotPhase4, ensure that ExtDataGasUsed is populated correctly.
	if rules.IsApricotPhase4 {
		// Make sure ExtDataGasUsed is not nil and correct
		if ethHeader.ExtDataGasUsed == nil {
			return errNilExtDataGasUsedApricotPhase4
		}
		if rules.IsApricotPhase5 {
			if ethHeader.ExtDataGasUsed.Cmp(params.AtomicGasLimit) == 1 {
				return fmt.Errorf("too large extDataGasUsed: %d", ethHeader.ExtDataGasUsed)
			}
		} else {
			if !ethHeader.ExtDataGasUsed.IsUint64() {
				return fmt.Errorf("too large extDataGasUsed: %d", ethHeader.ExtDataGasUsed)
			}
		}
		var totalGasUsed uint64
		for _, atomicTx := range atomicTxs {
			// We perform this check manually here to avoid the overhead of having to
			// reparse the atomicTx in `CalcExtDataGasUsed`.
			fixedFee := rules.IsApricotPhase5 // Charge the atomic tx fixed fee as of ApricotPhase5
			gasUsed, err := atomicTx.GasUsed(fixedFee)
			if err != nil {
				return err
			}
			totalGasUsed, err = safemath.Add(totalGasUsed, gasUsed)
			if err != nil {
				return err
			}
		}

		if ethHeader.ExtDataGasUsed.Cmp(new(big.Int).SetUint64(totalGasUsed)) != 0 {
			return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", ethHeader.ExtDataGasUsed, totalGasUsed)
		}
	}

	return nil
}

// SemanticVerify checks the semantic validity of the block. This is called the wrapper
// block manager's SemanticVerify method.
func (be *blockExtension) SemanticVerify(b extension.VMBlock) error {
	atomicTxs, err := extractAtomicTxsFromBlock(b, be.vm.Ethereum().BlockChain().Config())
	if err != nil {
		return err
	}
	return be.verifyUTXOsPresent(b, atomicTxs)
}

// OnAccept is called when the block is accepted. This is called the wrapper
// block manager's OnAccept method. The acceptedBatch contains the changes that
// were made to the database as a result of accepting the block, and it's flushed
// to the database in this method.
func (be *blockExtension) OnAccept(b extension.VMBlock, acceptedBatch database.Batch) error {
	atomicTxs, err := extractAtomicTxsFromBlock(b, be.vm.Ethereum().BlockChain().Config())
	if err != nil {
		return err
	}
	for _, tx := range atomicTxs {
		// Remove the accepted transaction from the mempool
		be.vm.mempool.RemoveTx(tx)
	}

	// Update VM state for atomic txs in this block. This includes updating the
	// atomic tx repo, atomic trie, and shared memory.
	atomicState, err := be.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Accept
		return err
	}
	// Apply any shared memory changes atomically with other pending batched changes
	return atomicState.Accept(acceptedBatch)
}

// OnReject is called when the block is rejected. This is called the wrapper
// block manager's OnReject method.
func (be *blockExtension) OnReject(b extension.VMBlock) error {
	atomicTxs, err := extractAtomicTxsFromBlock(b, be.vm.Ethereum().BlockChain().Config())
	if err != nil {
		return err
	}
	for _, tx := range atomicTxs {
		// Re-issue the transaction in the mempool, continue even if it fails
		be.vm.mempool.RemoveTx(tx)
		if err := be.vm.mempool.AddRemoteTx(tx); err != nil {
			log.Debug("Failed to re-issue transaction in rejected block", "txID", tx.ID(), "err", err)
		}
	}
	atomicState, err := be.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Reject
		return err
	}
	return atomicState.Reject()
}

// OnCleanup is called when the block is cleaned up. This is called the wrapper
// block manager's OnCleanup method.
func (be *blockExtension) OnCleanup(b extension.VMBlock) {
	if atomicState, err := be.vm.atomicBackend.GetVerifiedAtomicState(b.GetEthBlock().Hash()); err == nil {
		atomicState.Reject()
	}
}

// verifyUTXOsPresent returns an error if any of the atomic transactions name UTXOs that
// are not present in shared memory.
func (be *blockExtension) verifyUTXOsPresent(b extension.VMBlock, atomicTxs []*atomic.Tx) error {
	blockHash := common.Hash(b.ID())
	if be.vm.atomicBackend.IsBonus(b.Height(), blockHash) {
		log.Info("skipping atomic tx verification on bonus block", "block", blockHash)
		return nil
	}

	// verify UTXOs named in import txs are present in shared memory.
	for _, atomicTx := range atomicTxs {
		utx := atomicTx.UnsignedAtomicTx
		chainID, requests, err := utx.AtomicOps()
		if err != nil {
			return err
		}
		if _, err := be.vm.ctx.SharedMemory.Get(chainID, requests.RemoveRequests); err != nil {
			return fmt.Errorf("%w: %s", errMissingUTXOs, err)
		}
	}
	return nil
}

// extractAtomicTxsFromBlock extracts atomic transactions from the block's extra data.
func extractAtomicTxsFromBlock(b extension.VMBlock, chainConfig *params.ChainConfig) ([]*atomic.Tx, error) {
	ethBlock := b.GetEthBlock()
	if ethBlock == nil {
		return nil, errNilEthBlock
	}
	isApricotPhase5 := chainConfig.IsApricotPhase5(ethBlock.Time())
	atomicTxs, err := atomic.ExtractAtomicTxs(ethBlock.ExtData(), isApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, err
	}

	return atomicTxs, nil
}
