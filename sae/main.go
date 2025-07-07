// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/worstcase"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	atomictxpool "github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
)

const targetAtomicTxsSize = 40 * units.KiB

var (
	_ hook.Points = &hooks{}

	errEmptyBlock = errors.New("empty block")
)

type hooks struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig
	mempool     *atomictxpool.Mempool
}

func (h *hooks) GasTarget(parent *types.Block) gas.Gas {
	// TODO: implement me
	return acp176.MinTargetPerSecond
}

func (h *hooks) ConstructBlock(
	ctx context.Context,
	blockContext *block.Context,
	header *types.Header,
	parent *types.Header,
	ancestors iter.Seq[*types.Block],
	state hook.State,
	txs []*types.Transaction,
	receipts []*types.Receipt,
) (*types.Block, error) {
	ancestorInputUTXOs, err := inputUTXOs(ancestors)
	if err != nil {
		return nil, err
	}

	atomicTxs, err := packAtomicTxs(
		ctx,
		h.ctx.Log,
		state,
		h.ctx.AVAXAssetID,
		header.BaseFee,
		ancestorInputUTXOs,
		h.mempool,
	)
	if err != nil {
		return nil, err
	}

	// Blocks must either settle a prior transaction, include a new ethereum tx,
	// or include a new atomic tx.
	if header.GasUsed == 0 && len(txs) == 0 && len(atomicTxs) == 0 {
		return nil, errEmptyBlock
	}

	// TODO: This is where the block fee should be verified, do we still want to
	// utilize a block fee?

	atomicTxBytes, err := marshalAtomicTxs(atomicTxs)
	if err != nil {
		// If we fail to marshal the batch of atomic transactions for any
		// reason, discard the entire set of current transactions.
		h.ctx.Log.Debug("discarding txs due to error marshaling atomic transactions",
			zap.Error(err),
		)
		h.mempool.DiscardCurrentTxs()
		return nil, fmt.Errorf("failed to marshal batch of atomic transactions due to %w", err)
	}

	// TODO: What should we be doing with the ACP-176 logic here?
	//
	// chainConfigExtra := params.GetExtra(h.chainConfig)
	// extraPrefix, err := customheader.ExtraPrefix(chainConfigExtra, parent, header, nil) // TODO: Populate desired target excess
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to calculate new header.Extra: %w", err)
	// }

	rules := h.chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
	predicateResults, err := calculatePredicateResults(
		ctx,
		h.ctx,
		rules,
		blockContext,
		txs,
	)
	if err != nil {
		return nil, fmt.Errorf("calculatePredicateResults: %w", err)
	}

	predicateResultsBytes, err := predicateResults.Bytes()
	if err != nil {
		return nil, fmt.Errorf("predicateResults bytes: %w", err)
	}

	header.Extra = predicateResultsBytes // append(extraPrefix, predicateResultsBytes...)
	return customtypes.NewBlockWithExtData(
		header,
		txs,
		nil,
		receipts,
		trie.NewStackTrie(nil),
		atomicTxBytes,
		true,
	), nil
}

func (h *hooks) BlockExecuted(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	return nil // TODO: Implement me
}

func (h *hooks) ConstructBlockFromBlock(ctx context.Context, b *types.Block) (hook.ConstructBlock, error) {
	return func(context.Context, *block.Context, *types.Header, *types.Header, iter.Seq[*types.Block], hook.State, []*types.Transaction, []*types.Receipt) (*types.Block, error) {
		// TODO: Implement me
		return b, nil
	}, nil
}

func (h *hooks) ExtraBlockOperations(ctx context.Context, block *types.Block) ([]hook.Op, error) {
	txs, err := atomic.ExtractAtomicTxs(
		customtypes.BlockExtData(block),
		true,
		atomic.Codec,
	)
	if err != nil {
		return nil, err
	}

	baseFee := block.BaseFee()
	ops := make([]hook.Op, len(txs))
	for i, tx := range txs {
		op, err := atomicTxOp(tx, h.ctx.AVAXAssetID, baseFee)
		if err != nil {
			return nil, err
		}
		ops[i] = op
	}
	return ops, nil
}

func inputUTXOs(blocks iter.Seq[*types.Block]) (set.Set[ids.ID], error) {
	var inputUTXOs set.Set[ids.ID]
	for block := range blocks {
		// Extract atomic transactions from the block
		txs, err := atomic.ExtractAtomicTxs(
			customtypes.BlockExtData(block),
			true,
			atomic.Codec,
		)
		if err != nil {
			return nil, err
		}

		for _, tx := range txs {
			inputUTXOs.Union(tx.InputUTXOs())
		}
	}
	return inputUTXOs, nil
}

func packAtomicTxs(
	ctx context.Context,
	log logging.Logger,
	state hook.State,
	avaxAssetID ids.ID,
	baseFee *big.Int,
	ancestorInputUTXOs set.Set[ids.ID],
	mempool *atomictxpool.Mempool,
) ([]*atomic.Tx, error) {
	var (
		cumulativeSize int
		atomicTxs      []*atomic.Tx
	)
	for {
		tx, exists := mempool.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size
		// soft limit.
		txSize := len(tx.SignedBytes())
		if cumulativeSize+txSize > targetAtomicTxsSize {
			mempool.CancelCurrentTx(tx.ID())
			break
		}

		inputUTXOs := tx.InputUTXOs()
		if ancestorInputUTXOs.Overlaps(inputUTXOs) {
			// Discard the transaction from the mempool since it will fail
			// verification after this block has been accepted.
			//
			// Note: if the proposed block is not accepted, the transaction may
			// still be valid, but we discard it early here based on the
			// assumption that the proposed block will most likely be accepted.
			txID := tx.ID()
			log.Debug("discarding tx due to overlapping input utxos",
				zap.Stringer("txID", txID),
			)
			mempool.DiscardCurrentTx(txID)
			continue
		}

		op, err := atomicTxOp(tx, avaxAssetID, baseFee)
		if err != nil {
			mempool.DiscardCurrentTx(tx.ID())
			continue
		}

		err = state.Apply(op)
		if errors.Is(err, worstcase.ErrBlockTooFull) || errors.Is(err, worstcase.ErrQueueTooFull) {
			// Send [tx] back to the mempool's tx heap.
			mempool.CancelCurrentTx(tx.ID())
			break
		}
		if err != nil {
			txID := tx.ID()
			log.Debug("discarding tx from mempool due to failed verification",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			mempool.DiscardCurrentTx(txID)
			continue
		}

		atomicTxs = append(atomicTxs, tx)
		ancestorInputUTXOs.Union(inputUTXOs)

		cumulativeSize += txSize
	}
	return atomicTxs, nil
}

func atomicTxOp(
	tx *atomic.Tx,
	avaxAssetID ids.ID,
	baseFee *big.Int,
) (hook.Op, error) {
	// Note: we do not need to check if we are in at least ApricotPhase4 here
	// because we assume that this function will only be called when the block
	// is in at least ApricotPhase5.
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return hook.Op{}, err
	}
	burned, err := tx.Burned(avaxAssetID)
	if err != nil {
		return hook.Op{}, err
	}

	var bigGasUsed uint256.Int
	bigGasUsed.SetUint64(gasUsed)

	var gasPrice uint256.Int // gasPrice = burned * x2cRate / gasUsed
	gasPrice.SetUint64(burned)
	gasPrice.Mul(&gasPrice, atomic.X2CRate)
	gasPrice.Div(&gasPrice, &bigGasUsed)

	op := hook.Op{
		Gas:      gas.Gas(gasUsed),
		GasPrice: gasPrice,
	}
	switch tx := tx.UnsignedAtomicTx.(type) {
	case *atomic.UnsignedImportTx:
		op.To = make(map[common.Address]uint256.Int)
		for _, output := range tx.Outs {
			if output.AssetID != avaxAssetID {
				continue
			}
			var amount uint256.Int
			amount.SetUint64(output.Amount)
			amount.Mul(&amount, atomic.X2CRate)
			op.To[output.Address] = amount
		}
	case *atomic.UnsignedExportTx:
		op.From = make(map[common.Address]hook.Account)
		for _, input := range tx.Ins {
			if input.AssetID != avaxAssetID {
				continue
			}
			var amount uint256.Int
			amount.SetUint64(input.Amount)
			amount.Mul(&amount, atomic.X2CRate)
			op.From[input.Address] = hook.Account{
				Nonce:  input.Nonce,
				Amount: amount,
			}
		}
	default:
		return hook.Op{}, fmt.Errorf("unexpected atomic tx type: %T", tx)
	}
	return op, nil
}

func marshalAtomicTxs(txs []*atomic.Tx) ([]byte, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	return atomic.Codec.Marshal(atomic.CodecVersion, txs)
}

func calculatePredicateResults(
	ctx context.Context,
	snowContext *snow.Context,
	rules params.Rules,
	blockContext *block.Context,
	txs []*types.Transaction,
) (*predicate.Results, error) {
	predicateContext := precompileconfig.PredicateContext{
		SnowCtx:            snowContext,
		ProposerVMBlockCtx: blockContext,
	}
	predicateResults := predicate.NewResults()
	for _, tx := range txs {
		results, err := core.CheckPredicates(rules, &predicateContext, tx)
		if err != nil {
			return nil, err
		}
		predicateResults.SetTxResults(tx.Hash(), results)
	}
	return predicateResults, nil
}

func main() {
	rpcchainvm.Serve(context.Background(), &evm.VM{IsPlugin: true})
}
