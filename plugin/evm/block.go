// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/coreth/trie"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*Block)(nil)
	_ block.WithVerifyContext = (*Block)(nil)
)

var (
	apricotPhase0MinGasPrice = big.NewInt(params.LaunchMinGasPrice)
	apricotPhase1MinGasPrice = big.NewInt(params.ApricotPhase1MinGasPrice)
)

// Block implements the snowman.Block interface
type Block struct {
	id        ids.ID
	ethBlock  *types.Block
	extension extension.BlockExtension
	vm        *VM
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return b.id }

// SyntacticVerify verifies that a *Block is well-formed.
func (b *Block) SyntacticVerify() error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	header := b.ethBlock.Header()

	// Skip verification of the genesis block since it should already be marked as accepted.
	if b.ethBlock.Hash() == b.vm.genesisHash {
		return nil
	}

	rules := b.vm.chainConfig.Rules(header.Number, header.Time)
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

// SemanticVerify verifies that a *Block is internally consistent.
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

// Accept implements the snowman.Block interface
func (b *Block) Accept(context.Context) error {
	vm := b.vm

	// Although returning an error from Accept is considered fatal, it is good
	// practice to cleanup the batch we were modifying in the case of an error.
	defer vm.versiondb.Abort()

	log.Debug(fmt.Sprintf("Accepting block %s (%s) at height %d", b.ID().Hex(), b.ID(), b.Height()))

	// Call Accept for relevant precompile logs. Note we do this prior to
	// calling Accept on the blockChain so any side effects (eg warp signatures)
	// take place before the accepted log is emitted to subscribers.
	rules := vm.chainConfig.Rules(b.ethBlock.Number(), b.ethBlock.Timestamp())
	if err := b.handlePrecompileAccept(rules); err != nil {
		return err
	}
	if err := vm.blockChain.Accept(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not accept %s: %w", b.ID(), err)
	}

	if err := vm.PutLastAcceptedID(b.id); err != nil {
		return fmt.Errorf("failed to put %s as the last accepted block: %w", b.ID(), err)
	}

	// Get pending operations on the vm's versionDB so we can apply them atomically
	// with the block extension's changes.
	vdbBatch, err := vm.versiondb.CommitBatch()
	if err != nil {
		return fmt.Errorf("could not create commit batch processing block[%s]: %w", b.ID(), err)
	}

	if b.extension != nil {
		// Apply any changes atomically with other pending changes to
		// the vm's versionDB.
		return b.extension.OnAccept(b, vdbBatch)
	}

	return vdbBatch.Write()
}

// handlePrecompileAccept calls Accept on any logs generated with an active precompile address that implements
// contract.Accepter
func (b *Block) handlePrecompileAccept(rules params.Rules) error {
	vm := b.vm
	// Short circuit early if there are no precompile accepters to execute
	if len(rules.AccepterPrecompiles) == 0 {
		return nil
	}

	// Read receipts from disk
	receipts := rawdb.ReadReceipts(vm.chaindb, b.ethBlock.Hash(), b.ethBlock.NumberU64(), b.ethBlock.Time(), vm.chainConfig)
	// If there are no receipts, ReadReceipts may be nil, so we check the length and confirm the ReceiptHash
	// is empty to ensure that missing receipts results in an error on accept.
	if len(receipts) == 0 && b.ethBlock.ReceiptHash() != types.EmptyRootHash {
		return fmt.Errorf("failed to fetch receipts for accepted block with non-empty root hash (%s) (Block: %s, Height: %d)", b.ethBlock.ReceiptHash(), b.ethBlock.Hash(), b.ethBlock.NumberU64())
	}
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: vm.ctx,
		Warp:    vm.warpBackend,
	}
	for _, receipt := range receipts {
		for logIdx, log := range receipt.Logs {
			accepter, ok := rules.AccepterPrecompiles[log.Address]
			if !ok {
				continue
			}
			if err := accepter.Accept(acceptCtx, log.BlockHash, log.BlockNumber, log.TxHash, logIdx, log.Topics, log.Data); err != nil {
				return err
			}
		}
	}

	return nil
}

// Reject implements the snowman.Block interface
// If [b] contains an atomic transaction, attempt to re-issue it
func (b *Block) Reject(context.Context) error {
	log.Debug(fmt.Sprintf("Rejecting block %s (%s) at height %d", b.ID().Hex(), b.ID(), b.Height()))

	if err := b.vm.blockChain.Reject(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not reject %s: %w", b.ID(), err)
	}

	if b.extension != nil {
		return b.extension.OnReject(b)
	}
	return nil
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() ids.ID {
	return ids.ID(b.ethBlock.ParentHash())
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.ethBlock.NumberU64()
}

// Timestamp implements the snowman.Block interface
func (b *Block) Timestamp() time.Time {
	return time.Unix(int64(b.ethBlock.Time()), 0)
}

// Verify implements the snowman.Block interface
func (b *Block) Verify(context.Context) error {
	return b.verify(&precompileconfig.PredicateContext{
		SnowCtx:            b.vm.ctx,
		ProposerVMBlockCtx: nil,
	}, true)
}

// ShouldVerifyWithContext implements the block.WithVerifyContext interface
func (b *Block) ShouldVerifyWithContext(context.Context) (bool, error) {
	predicates := b.vm.chainConfig.Rules(b.ethBlock.Number(), b.ethBlock.Timestamp()).Predicaters
	// Short circuit early if there are no predicates to verify
	if len(predicates) == 0 {
		return false, nil
	}

	// Check if any of the transactions in the block specify a precompile that enforces a predicate, which requires
	// the ProposerVMBlockCtx.
	for _, tx := range b.ethBlock.Transactions() {
		for _, accessTuple := range tx.AccessList() {
			if _, ok := predicates[accessTuple.Address]; ok {
				log.Debug("Block verification requires proposerVM context", "block", b.ID(), "height", b.Height())
				return true, nil
			}
		}
	}

	log.Debug("Block verification does not require proposerVM context", "block", b.ID(), "height", b.Height())
	return false, nil
}

// VerifyWithContext implements the block.WithVerifyContext interface
func (b *Block) VerifyWithContext(ctx context.Context, proposerVMBlockCtx *block.Context) error {
	return b.verify(&precompileconfig.PredicateContext{
		SnowCtx:            b.vm.ctx,
		ProposerVMBlockCtx: proposerVMBlockCtx,
	}, true)
}

// Verify the block is valid.
// Enforces that the predicates are valid within [predicateContext].
// Writes the block details to disk and the state to the trie manager iff writes=true.
func (b *Block) verify(predicateContext *precompileconfig.PredicateContext, writes bool) error {
	if predicateContext.ProposerVMBlockCtx != nil {
		log.Debug("Verifying block with context", "block", b.ID(), "height", b.Height())
	} else {
		log.Debug("Verifying block without context", "block", b.ID(), "height", b.Height())
	}
	if err := b.SyntacticVerify(); err != nil {
		return fmt.Errorf("syntactic block verification failed: %w", err)
	}

	// Only enforce predicates if the chain has already bootstrapped.
	// If the chain is still bootstrapping, we can assume that all blocks we are verifying have
	// been accepted by the network (so the predicate was validated by the network when the
	// block was originally verified).
	if b.vm.bootstrapped.Get() {
		if err := b.verifyPredicates(predicateContext); err != nil {
			return fmt.Errorf("failed to verify predicates: %w", err)
		}
	}

	if err := b.SemanticVerify(); err != nil {
		return fmt.Errorf("failed to verify block extension: %w", err)
	}

	// The engine may call VerifyWithContext multiple times on the same block with different contexts.
	// Since the engine will only call Accept/Reject once, we should only call InsertBlockManual once.
	// Additionally, if a block is already in processing, then it has already passed verification and
	// at this point we have checked the predicates are still valid in the different context so we
	// can return nil.
	if b.vm.State.IsProcessing(b.id) {
		return nil
	}

	err := b.vm.blockChain.InsertBlockManual(b.ethBlock, writes)
	if b.extension != nil && (err != nil || !writes) {
		b.extension.CleanupVerified(b)
	}
	return err
}

// verifyPredicates verifies the predicates in the block are valid according to predicateContext.
func (b *Block) verifyPredicates(predicateContext *precompileconfig.PredicateContext) error {
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), b.ethBlock.Timestamp())

	switch {
	case !rules.IsDurango && rules.PredicatersExist():
		return errors.New("cannot enable predicates before Durango activation")
	case !rules.IsDurango:
		return nil
	}

	predicateResults := predicate.NewResults()
	for _, tx := range b.ethBlock.Transactions() {
		results, err := core.CheckPredicates(rules, predicateContext, tx)
		if err != nil {
			return err
		}
		predicateResults.SetTxResults(tx.Hash(), results)
	}
	// TODO: document required gas constraints to ensure marshalling predicate results does not error
	predicateResultsBytes, err := predicateResults.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal predicate results: %w", err)
	}
	extraData := b.ethBlock.Extra()
	headerPredicateResultsBytes, ok := predicate.GetPredicateResultBytes(extraData)
	if !ok {
		return fmt.Errorf("failed to find predicate results in extra data: %x", extraData)
	}
	if !bytes.Equal(headerPredicateResultsBytes, predicateResultsBytes) {
		return fmt.Errorf("%w (remote: %x local: %x)", errInvalidHeaderPredicateResults, headerPredicateResultsBytes, predicateResultsBytes)
	}
	return nil
}

// Bytes implements the snowman.Block interface
func (b *Block) Bytes() []byte {
	res, err := rlp.EncodeToBytes(b.ethBlock)
	if err != nil {
		panic(err)
	}
	return res
}

func (b *Block) String() string { return fmt.Sprintf("EVM block, ID = %s", b.ID()) }

func (b *Block) GetEthBlock() *types.Block {
	return b.ethBlock
}
