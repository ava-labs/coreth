// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*Block)(nil)
	_ block.WithVerifyContext = (*Block)(nil)
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

// semanticVerify verifies that a *Block is internally consistent.
func (b *Block) semanticVerify() error {
	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}

	if b.extension != nil {
		if err := b.extension.SemanticVerify(b); err != nil {
			return err
		}
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
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), params.IsMergeTODO, b.ethBlock.Time())
	if err := b.handlePrecompileAccept(*params.GetRulesExtra(rules)); err != nil {
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
		// OnAccept flushes the changes in the batch to the database.
		// TODO: Should we rename this to WriteBatch?
		return b.extension.OnAccept(b, vdbBatch)
	}

	// If there is no extension, we still need to apply the changes to the versionDB
	return vdbBatch.Write()
}

// handlePrecompileAccept calls Accept on any logs generated with an active precompile address that implements
// contract.Accepter
func (b *Block) handlePrecompileAccept(rules extras.Rules) error {
	// Short circuit early if there are no precompile accepters to execute
	if len(rules.AccepterPrecompiles) == 0 {
		return nil
	}

	// Read receipts from disk
	receipts := rawdb.ReadReceipts(b.vm.chaindb, b.ethBlock.Hash(), b.ethBlock.NumberU64(), b.ethBlock.Time(), b.vm.chainConfig)
	// If there are no receipts, ReadReceipts may be nil, so we check the length and confirm the ReceiptHash
	// is empty to ensure that missing receipts results in an error on accept.
	if len(receipts) == 0 && b.ethBlock.ReceiptHash() != types.EmptyRootHash {
		return fmt.Errorf("failed to fetch receipts for accepted block with non-empty root hash (%s) (Block: %s, Height: %d)", b.ethBlock.ReceiptHash(), b.ethBlock.Hash(), b.ethBlock.NumberU64())
	}
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: b.vm.ctx,
		Warp:    b.vm.warpBackend,
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
	if b.extension != nil {
		if err := b.extension.OnReject(b); err != nil {
			return err
		}
	}
	return b.vm.blockChain.Reject(b.ethBlock)
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
	rules := params.GetRulesExtra(b.vm.chainConfig.Rules(b.ethBlock.Number(), params.IsMergeTODO, b.ethBlock.Time()))
	predicates := rules.Predicaters
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
	if err := b.syntacticVerify(); err != nil {
		return fmt.Errorf("syntactic block verification failed: %w", err)
	}

	if err := b.semanticVerify(); err != nil {
		return fmt.Errorf("failed to verify block extension: %w", err)
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
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), params.IsMergeTODO, b.ethBlock.Time())
	rulesExtra := params.GetRulesExtra(rules)

	switch {
	case !rulesExtra.IsDurango && rulesExtra.PredicatersExist():
		return errors.New("cannot enable predicates before Durango activation")
	case !rulesExtra.IsDurango:
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
	avalancheRules := params.GetRulesExtra(rules).AvalancheRules
	headerPredicateResultsBytes := header.PredicateBytesFromExtra(avalancheRules, extraData)
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
