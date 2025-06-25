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
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/worstcase"
	"github.com/golang/gddo/log"
	"k8s.io/utils/set"

	atomictxpool "github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
)

const targetAtomicTxsSize = 40 * units.KiB

var _ hook.Points = &hooks{}

type hooks struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig
	mempool     *atomictxpool.Mempool
}

func (h *hooks) GasTarget(parent *types.Block) gas.Gas {
	state, err := acp176.ParseState(parent.Extra())
	if err != nil {
		panic(err)
	}
	return state.Target()
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
	cumulativeInputUTXOs, err := inputUTXOs(ancestors)
	if err != nil {
		return nil, err
	}

	var (
		cumulativeSize    int
		atomicTxs         []*atomic.Tx
		batchContribution = big.NewInt(0)
		gasUsed           gas.Gas
	)
	for {
		tx, exists := h.mempool.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size
		// soft limit.
		txSize := len(tx.SignedBytes())
		if cumulativeSize+txSize > targetAtomicTxsSize {
			h.mempool.CancelCurrentTx(tx.ID())
			break
		}

		inputUTXOs := tx.InputUTXOs()
		if cumulativeInputUTXOs.Overlaps(inputUTXOs) {
			// Discard the transaction from the mempool since it will fail
			// verification after this block has been accepted.
			//
			// Note: if the proposed block is not accepted, the transaction may
			// still be valid, but we discard it early here based on the
			// assumption that the proposed block will most likely be accepted.
			log.Debug("discarding tx due to overlapping input utxos", "txID", tx.ID())
			h.mempool.DiscardCurrentTx(tx.ID())
			continue
		}

		var (
			txGasUsed, txContribution *big.Int
			err                       error
		)

		// Note: we do not need to check if we are in at least ApricotPhase4
		// here because we assume that this function will only be called when
		// the block is in at least ApricotPhase5.
		txContribution, txGasUsed, err = tx.BlockFeeContribution(true, h.ctx.AVAXAssetID, header.BaseFee)
		if err != nil {
			return nil, err
		}

		// TODO: Make this correctly
		op := hook.Op{
			Gas: gas.Gas(txGasUsed.Uint64()),
		}
		err = state.Apply(op)
		if errors.Is(err, worstcase.ErrBlockTooFull) || errors.Is(err, worstcase.ErrQueueTooFull) {
			// Send [tx] back to the mempool's tx heap.
			h.mempool.CancelCurrentTx(tx.ID())
			break
		}
		if err != nil {
			log.Debug("discarding tx from mempool due to failed verification",
				"txID", tx.ID(),
				"err", err,
			)
			h.mempool.DiscardCurrentTx(tx.ID())
			continue
		}

		atomicTxs = append(atomicTxs, tx)
		cumulativeInputUTXOs.Union(inputUTXOs)

		// Add the [txGasUsed] to the [batchGasUsed] when the [tx] has passed verification
		gasUsed += gas.Gas(txGasUsed.Uint64())
		batchContribution.Add(batchContribution, txContribution)
		cumulativeSize += txSize
	}

	// If there is a non-zero number of transactions, marshal them and return the byte slice
	// for the block's extra data along with the contribution and gas used.
	if len(atomicTxs) > 0 {
		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, atomicTxs)
		if err != nil {
			// If we fail to marshal the batch of atomic transactions for any reason,
			// discard the entire set of current transactions.
			log.Debug("discarding txs due to error marshaling atomic transactions", "err", err)
			vm.mempool.DiscardCurrentTxs()
			return nil, nil, nil, fmt.Errorf("failed to marshal batch of atomic transactions due to %w", err)
		}
		return atomicTxBytes, batchContribution, batchGasUsed, nil
	}

	// If there are no regular transactions and there were also no atomic
	// transactions to be included, then the block is empty and should be
	// considered invalid.
	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, atomicvm.ErrEmptyBlock
	}

	// If there are no atomic transactions, but there is a non-zero number of regular transactions, then
	// we return a nil slice with no contribution from the atomic transactions and a nil error.
	return nil, nil, nil, nil

}

func (h *hooks) BlockExecuted(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	panic("unimplemented")
}

func (h *hooks) ConstructBlockFromBlock(ctx context.Context, block *types.Block) (hook.ConstructBlock, error) {
	panic("unimplemented")
}

func (h *hooks) ExtraBlockOperations(ctx context.Context, block *types.Block) ([]hook.Op, error) {
	panic("unimplemented")
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

func main() {
	rpcchainvm.Serve(context.Background(), &evm.VM{IsPlugin: true})
}
