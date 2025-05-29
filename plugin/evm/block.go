// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	customheader "github.com/ava-labs/coreth/plugin/evm/header"
)

var (
	_ snowman.Block           = (*Block)(nil)
	_ block.WithVerifyContext = (*Block)(nil)
)

var errMissingUTXOs = errors.New("missing UTXOs")

// readMainnetBonusBlocks returns maps of bonus block numbers to block IDs.
// Note bonus blocks are indexed in the atomic trie.
func readMainnetBonusBlocks() (map[uint64]ids.ID, error) {
	mainnetBonusBlocks := map[uint64]string{
		102972: "Njm9TcLUXRojZk8YhEM6ksvfiPdC1TME4zJvGaDXgzMCyB6oB",
		103105: "BYqLB6xpqy7HsAgP2XNfGE8Ubg1uEzse5mBPTSJH9z5s8pvMa",
		103143: "AfWvJH3rB2fdHuPWQp6qYNCFVT29MooQPRigD88rKKwUDEDhq",
		103183: "2KPW9G5tiNF14tZNfG4SqHuQrtUYVZyxuof37aZ7AnTKrQdsHn",
		103197: "pE93VXY3N5QKfwsEFcM9i59UpPFgeZ8nxpJNaGaDQyDgsscNf",
		103203: "2czmtnBS44VCWNRFUM89h4Fe9m3ZeZVYyh7Pe3FhNqjRNgPXhZ",
		103208: "esx5J962LtYm2aSrskpLai5e4CMMsaS1dsu9iuLGJ3KWgSu2M",
		103209: "DK9NqAJGry1wAo767uuYc1dYXAjUhzwka6vi8d9tNheqzGUTd",
		103259: "i1HoerJ1axognkUKKL58FvF9aLrbZKtv7TdKLkT5kgzoeU1vB",
		103261: "2DpCuBaH94zKKFNY2XTs4GeJcwsEv6qT2DHc59S8tdg97GZpcJ",
		103266: "2ez4CA7w4HHr8SSobHQUAwFgj2giRNjNFUZK9JvrZFa1AuRj6X",
		103287: "2QBNMMFJmhVHaGF45GAPszKyj1gK6ToBERRxYvXtM7yfrdUGPK",
		103339: "2pSjfo7rkFCfZ2CqAxqfw8vqM2CU2nVLHrFZe3rwxz43gkVuGo",
		103346: "2SiSziHHqPjb1qkw7CdGYupokiYpd2b7mMqRiyszurctcA5AKr",
		103350: "2F5tSQbdTfhZxvkxZqdFp7KR3FrJPKEsDLQK7KtPhNXj1EZAh4",
		103358: "2tCe88ur6MLQcVgwE5XxoaHiTGtSrthwKN3SdbHE4kWiQ7MSTV",
		103437: "21o2fVTnzzmtgXqkV1yuQeze7YEQhR5JB31jVVD9oVUnaaV8qm",
		103472: "2nG4exd9eUoAGzELfksmBR8XDCKhohY1uDKRFzEXJG4M8p3qA7",
		103478: "63YLdYXfXc5tY3mwWLaDsbXzQHYmwWVxMP7HKbRh4Du3C2iM1",
		103493: "soPweZ8DGaoUMjrnzjH3V2bypa7ZvvfqBan4UCsMUxMP759gw",
		103514: "2dNkpQF4mooveyUDfBYQTBfsGDV4wkncQPpEw4kHKfSTSTo5x",
		103536: "PJTkRrHvKZ1m4AQdPND1MBpUXpCrGN4DDmXmJQAiUrsxPoLQX",
		103545: "22ck2Z7cC38hmBfX2v3jMWxun8eD8psNaicfYeokS67DxwmPTx",
		103547: "pTf7gfk1ksj7bqMrLyMCij8FBKth1uRqQrtfykMFeXhx5xnrL",
		103554: "9oZh4qyBCcVwSGyDoUzRAuausvPJN3xH6nopKS6bwYzMfLoQ2",
		103555: "MjExz2z1qhwugc1tAyiGxRsCq4GvJwKfyyS29nr4tRVB8ooic",
		103559: "cwJusfmn98TW3DjAbfLRN9utYR24KAQ82qpAXmVSvjHyJZuM2",
		103561: "2YgxGHns7Z2hMMHJsPCgVXuJaL7x1b3gnHbmSCfCdyAcYGr6mx",
		103563: "2AXxT3PSEnaYHNtBTnYrVTf24TtKDWjky9sqoFEhydrGXE9iKH",
		103564: "Ry2sfjFfGEnJxRkUGFSyZNn7GR3m4aKAf1scDW2uXSNQB568Y",
		103569: "21Jys8UNURmtckKSV89S2hntEWymJszrLQbdLaNcbXcxDAsQSa",
		103570: "sg6wAwFBsPQiS5Yfyh41cVkCRQbrrXsxXmeNyQ1xkunf2sdyv",
		103575: "z3BgePPpCXq1mRBRvUi28rYYxnEtJizkUEHnDBrcZeVA7MFVk",
		103577: "uK5Ff9iBfDtREpVv9NgCQ1STD1nzLJG3yrfibHG4mGvmybw6f",
		103578: "Qv5v5Ru8ArfnWKB1w6s4G5EYPh7TybHJtF6UsVwAkfvZFoqmj",
		103582: "7KCZKBpxovtX9opb7rMRie9WmW5YbZ8A4HwBBokJ9eSHpZPqx",
		103587: "2AfTQ2FXNj9bkSUQnud9pFXULx6EbF7cbbw6i3ayvc2QNhgxfF",
		103590: "2gTygYckZgFZfN5QQWPaPBD3nabqjidV55mwy1x1Nd4JmJAwaM",
		103591: "2cUPPHy1hspr2nAKpQrrAEisLKkaWSS9iF2wjNFyFRs8vnSkKK",
		103594: "5MptSdP6dBMPSwk9GJjeVe39deZJTRh9i82cgNibjeDffrrTf",
		103597: "2J8z7HNv4nwh82wqRGyEHqQeuw4wJ6mCDCSvUgusBu35asnshK",
		103598: "2i2FP6nJyvhX9FR15qN2D9AVoK5XKgBD2i2AQ7FoSpfowxvQDX",
		103603: "2v3smb35s4GLACsK4Zkd2RcLBLdWA4huqrvq8Y3VP4CVe8kfTM",
		103604: "b7XfDDLgwB12DfL7UTWZoxwBpkLPL5mdHtXngD94Y2RoeWXSh",
		103607: "PgaRk1UAoUvRybhnXsrLq5t6imWhEa6ksNjbN6hWgs4qPrSzm",
		103612: "2oueNTj4dUE2FFtGyPpawnmCCsy6EUQeVHVLZy8NHeQmkAciP4",
		103614: "2YHZ1KymFjiBhpXzgt6HXJhLSt5SV9UQ4tJuUNjfN1nQQdm5zz",
		103617: "amgH2C1s9H3Av7vSW4y7n7TXb9tKyKHENvrDXutgNN6nsejgc",
		103618: "fV8k1U8oQDmfVwK66kAwN73aSsWiWhm8quNpVnKmSznBycV2W",
		103621: "Nzs93kFTvcXanFUp9Y8VQkKYnzmH8xykxVNFJTkdyAEeuxWbP",
		103623: "2rAsBj3emqQa13CV8r5fTtHogs4sXnjvbbXVzcKPi3WmzhpK9D",
		103624: "2JbuExUGKW5mYz5KfXATwq1ibRDimgks9wEdYGNSC6Ttey1R4U",
		103627: "tLLijh7oKfvWT1yk9zRv4FQvuQ5DAiuvb5kHCNN9zh4mqkFMG",
		103628: "dWBsRYRwFrcyi3DPdLoHsL67QkZ5h86hwtVfP94ZBaY18EkmF",
		103629: "XMoEsew2DhSgQaydcJFJUQAQYP8BTNTYbEJZvtbrV2QsX7iE3",
		103630: "2db2wMbVAoCc5EUJrsBYWvNZDekqyY8uNpaaVapdBAQZ5oRaou",
		103633: "2QiHZwLhQ3xLuyyfcdo5yCUfoSqWDvRZox5ECU19HiswfroCGp",
	}

	bonusBlockMainnetHeights := make(map[uint64]ids.ID)
	for height, blkIDStr := range mainnetBonusBlocks {
		blkID, err := ids.FromString(blkIDStr)
		if err != nil {
			return nil, err
		}
		bonusBlockMainnetHeights[height] = blkID
	}
	return bonusBlockMainnetHeights, nil
}

// Block implements the snowman.Block interface
type Block struct {
	ethBlock  *types.Block
	vm        *VM
	atomicTxs []*atomic.Tx
}

// newBlock returns a new Block wrapping the ethBlock type and implementing the snowman.Block interface
func (vm *VM) newBlock(ethBlock *types.Block) (*Block, error) {
	isApricotPhase5 := vm.chainConfigExtra().IsApricotPhase5(ethBlock.Time())
	atomicTxs, err := atomic.ExtractAtomicTxs(customtypes.BlockExtData(ethBlock), isApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, err
	}

	return &Block{
		ethBlock:  ethBlock,
		vm:        vm,
		atomicTxs: atomicTxs,
	}, nil
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return ids.ID(b.ethBlock.Hash()) }

func (b *Block) AtomicTxs() []*atomic.Tx { return b.atomicTxs }

// Accept implements the snowman.Block interface
func (b *Block) Accept(context.Context) error {
	vm := b.vm

	// Although returning an error from Accept is considered fatal, it is good
	// practice to cleanup the batch we were modifying in the case of an error.
	defer vm.versiondb.Abort()

	blkID := b.ID()
	log.Debug("accepting block",
		"hash", blkID.Hex(),
		"id", blkID,
		"height", b.Height(),
	)

	// Call Accept for relevant precompile logs. Note we do this prior to
	// calling Accept on the blockChain so any side effects (eg warp signatures)
	// take place before the accepted log is emitted to subscribers.
	rules := b.vm.rules(b.ethBlock.Number(), b.ethBlock.Time())
	if err := b.handlePrecompileAccept(rules); err != nil {
		return err
	}
	if err := vm.blockChain.Accept(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not accept %s: %w", blkID, err)
	}

	if err := vm.acceptedBlockDB.Put(lastAcceptedKey, blkID[:]); err != nil {
		return fmt.Errorf("failed to put %s as the last accepted block: %w", blkID, err)
	}

	for _, tx := range b.atomicTxs {
		// Remove the accepted transaction from the mempool
		vm.mempool.RemoveTx(tx)
	}

	// Update VM state for atomic txs in this block. This includes updating the
	// atomic tx repo, atomic trie, and shared memory.
	atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(blkID))
	if err != nil {
		// should never occur since [b] must be verified before calling Accept
		return err
	}
	// Get pending operations on the vm's versionDB so we can apply them atomically
	// with the shared memory changes.
	vdbBatch, err := b.vm.versiondb.CommitBatch()
	if err != nil {
		return fmt.Errorf("could not create commit batch processing block[%s]: %w", blkID, err)
	}

	// Apply any shared memory changes atomically with other pending changes to
	// the vm's versionDB.
	return atomicState.Accept(vdbBatch)
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
	blkID := b.ID()
	log.Debug("rejecting block",
		"hash", blkID.Hex(),
		"id", blkID,
		"height", b.Height(),
	)

	for _, tx := range b.atomicTxs {
		// Re-issue the transaction in the mempool, continue even if it fails
		b.vm.mempool.RemoveTx(tx)
		if err := b.vm.mempool.AddRemoteTx(tx); err != nil {
			log.Debug("Failed to re-issue transaction in rejected block", "txID", tx.ID(), "err", err)
		}
	}
	atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(blkID))
	if err != nil {
		// should never occur since [b] must be verified before calling Reject
		return err
	}
	if err := atomicState.Reject(); err != nil {
		return err
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
	rules := b.vm.rules(b.ethBlock.Number(), b.ethBlock.Time())
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
func (b *Block) verify(context *precompileconfig.PredicateContext, writes bool) error {
	log.Debug("verifying block",
		"hash", b.ethBlock.Hash(),
		"height", b.ethBlock.NumberU64(),
		"hasContext", context.ProposerVMBlockCtx != nil,
		"writes", writes,
	)

	if err := b.verifyWithoutParent(); err != nil {
		return fmt.Errorf("verifyWithoutParent: %w", err)
	}
	if err := b.verifyCanExecute(context); err != nil {
		return fmt.Errorf("verifyCanExecute: %w", err)
	}
	if err := b.execute(context, writes); err != nil {
		return fmt.Errorf("execute: %w", err)
	}
	return nil
}

func (b *Block) verifyWithoutParent() error {
	return verifyBlockStandalone(
		b.vm.syntacticBlockValidator.extDataHashes,
		&b.vm.clock,
		b.vm.chainConfig,
		b.ethBlock,
		b.atomicTxs,
	)
}

// Assumes [Block.verifyWithoutParent] has returned nil.
func (b *Block) verifyCanExecute(context *precompileconfig.PredicateContext) error {
	// If the VM is not marked as bootstrapped the other chains may also be
	// bootstrapping and not have populated the required indices. Since
	// bootstrapping only verifies blocks that have been canonically accepted by
	// the network, these checks would be guaranteed to pass on a synced node.
	if b.vm.bootstrapped.Get() {
		// Verify that the atomic txs in this block are valid to be executed.
		if err := b.verifyAtomicTxs(); err != nil {
			return err
		}

		// Verify that all the ICM messages are correctly marked as either valid
		// or invalid.
		if err := b.verifyPredicates(context); err != nil {
			return fmt.Errorf("failed to verify predicates: %w", err)
		}
	}

	header := b.ethBlock.Header()
	number := header.Number.Uint64()
	parentHeader := b.vm.blockChain.GetHeader(header.ParentHash, number-1)
	if parentHeader == nil {
		return consensus.ErrUnknownAncestor
	}

	parentNumber := parentHeader.Number.Uint64()
	if expectedNumber := parentNumber + 1; number != expectedNumber {
		return fmt.Errorf("expected number %d, found %d", expectedNumber, number)
	}

	// Verify the header's timestamp is not earlier than parent's. It allows
	// equality(==), multiple blocks can have the same timestamp.
	if header.Time < parentHeader.Time {
		return fmt.Errorf("invalid timestamp %d < parent timestamp %d", header.Time, parentHeader.Time)
	}

	config := params.GetExtra(b.vm.chainConfig)
	if err := customheader.VerifyGasLimit(config, parentHeader, header); err != nil {
		return err
	}

	expectedBaseFee, err := customheader.BaseFee(config, parentHeader, header.Time)
	if err != nil {
		return fmt.Errorf("failed to calculate base fee: %w", err)
	}
	if !utils.BigEqual(header.BaseFee, expectedBaseFee) {
		return fmt.Errorf("expected base fee %d, found %d", expectedBaseFee, header.BaseFee)
	}

	// Verify the BlockGasCost set in the header matches the expected value.
	expectedBlockGasCost := customheader.BlockGasCost(
		config,
		parentHeader,
		header.Time,
	)
	headerExtra := customtypes.GetHeaderExtra(header)
	if !utils.BigEqual(headerExtra.BlockGasCost, expectedBlockGasCost) {
		return fmt.Errorf("invalid blockGasCost: have %d, want %d", headerExtra.BlockGasCost, expectedBlockGasCost)
	}

	// These checks assume that GasUsed is set correctly. GasUsed still needs to
	// be verified after execution.
	if err := customheader.VerifyGasUsed(config, parentHeader, header); err != nil {
		return err
	}
	if err := customheader.VerifyExtraPrefix(config, parentHeader, header); err != nil {
		return err
	}

	// Ancestor block must be known.
	if !b.vm.blockChain.HasBlockAndState(header.ParentHash, parentNumber) {
		return fmt.Errorf("parent block or state not available: hash=%x, number=%d", header.ParentHash, parentNumber)
	}

	// Things still left to be verified:
	// - Root hash
	// - ReceiptHash
	// - Bloom
	// - GasUsed
	// - BlockGasCost is paid for
	// - Normal eth tx checks:
	//   - Sender can be recovered correctly (verifies chainID)
	//   - Tx nonces are correct (matches what is in state and won't cause an overflow)
	//   - Sender is an EOA (probably removed with 7702, also was really not possible before)
	//   - Tx's GasFeeCap is >= the BaseFee
	//   - Tx can afford the gas based on the gas limit and effective gas price
	//   - Tx doesn't specify a GasLimit which exceeds the gas remaining allowed to process in the block
	//   - Tx's gas limit is >= the intrinsic gas of the transaction
	//   - Tx is able to send the value of the transaction from the sender after purchasing the gas
	//   - The message data doesn't exceed MaxInitCodeSize when creating a contract (post Durango)
	return nil
}

// Assumes [Block.verifyCanExecute] has returned nil.
func (b *Block) execute(context *precompileconfig.PredicateContext, writes bool) error {
	// The engine may call VerifyWithContext multiple times on the same block
	// with different contexts. Since the engine will only call Accept/Reject
	// once, we should only call InsertBlockManual once. Additionally, if a
	// block is already in processing, then it has already passed verification
	// and at this point we have checked the predicates are still valid in the
	// different context so we can return nil.
	hash := b.ethBlock.Hash()
	if b.vm.State.IsProcessing(ids.ID(hash)) {
		return nil
	}

	if writes {
		// Update the atomic backend with [txs] from this block.
		//
		// Note: The atomic trie canonically contains the duplicate operations from
		// any bonus blocks.
		var (
			number     = b.ethBlock.NumberU64()
			parentHash = b.ethBlock.ParentHash()
		)
		if _, err := b.vm.atomicBackend.InsertTxs(hash, number, parentHash, b.atomicTxs); err != nil {
			return err
		}
	}

	err := b.vm.blockChain.InsertBlockManual(b.ethBlock, writes)
	if err != nil && writes {
		// Unpin the atomic trie if it is pinned and verification failed.
		if atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(hash); err == nil {
			_ = atomicState.Reject() // ignore this error so we can return the original error instead.
		}
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
	avalancheRules := rulesExtra.AvalancheRules
	headerPredicateResultsBytes := header.PredicateBytesFromExtra(avalancheRules, extraData)
	if !bytes.Equal(headerPredicateResultsBytes, predicateResultsBytes) {
		return fmt.Errorf("%w (remote: %x local: %x)", errInvalidHeaderPredicateResults, headerPredicateResultsBytes, predicateResultsBytes)
	}
	return nil
}

// verifyAtomicTxs verifies that the atomic txs consumed by the block are valid.
func (b *Block) verifyAtomicTxs() error {
	hash := b.ethBlock.Hash()
	if b.vm.atomicBackend.IsBonus(b.Height(), hash) {
		log.Info("skipping atomic tx verification on bonus block",
			"block", hash,
		)
		return nil
	}

	// Verify [txs] do not conflict with themselves or ancestor blocks.
	var (
		parentHash = b.ethBlock.ParentHash()
		baseFee    = b.ethBlock.BaseFee()
		number     = b.ethBlock.Number()
		time       = b.ethBlock.Time()
		rules      = b.vm.chainConfig.Rules(number, params.IsMergeTODO, time)
		rulesExtra = params.GetRulesExtra(rules)
	)
	// TODO: Pass rulesExtra as a pointer
	return b.vm.verifyTxs(b.atomicTxs, parentHash, baseFee, number.Uint64(), *rulesExtra)
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
