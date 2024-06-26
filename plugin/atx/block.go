package atx

import (
	"context"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	snowmanblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type SharedMemoryWriter interface {
	AddSharedMemoryRequests(chainID ids.ID, requests *atomic.Requests)
}

type BlockWithVerifyContext interface {
	snowman.Block
	snowmanblock.WithVerifyContext
}

type BlockImpl struct {
	BlockWithVerifyContext

	Txs                []*Tx
	SharedMemoryWriter SharedMemoryWriter
	VM                 *VM
}

func (b *BlockImpl) Accept(ctx context.Context) error {
	vm := b.VM
	for _, tx := range b.Txs {
		// Remove the accepted transaction from the mempool
		vm.Mempool().RemoveTx(tx)
	}

	// Update VM state for atomic txs in this block. This includes updating the
	// atomic tx repo, atomic trie, and shared memory.
	atomicState, err := vm.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Accept
		return err
	}
	if err := atomicState.Accept(b.SharedMemoryWriter); err != nil {
		return err
	}
	if err := b.BlockWithVerifyContext.Accept(ctx); err != nil {
		// reinject txs to backend if accept fails
		blk := b.BlockWithVerifyContext
		if _, err := b.VM.atomicBackend.InsertTxs(common.Hash(blk.ID()), blk.Height(), common.Hash(blk.Parent()), b.Txs); err != nil {
			log.Error("Failed to re-inject transactions in accepted block", "blockID", blk.ID(), "err", err)
		}
		return err
	}

	return nil
}

func (b *BlockImpl) VerifyWithContext(ctx context.Context, proposerVMBlockCtx *snowmanblock.Context) error {
	// verify UTXOs named in import txs are present in shared memory.
	if err := b.VM.VerifyUTXOsPresent(common.Hash(b.ID()), b.Height(), b.Txs); err != nil {
		return err
	}
	blk := b.BlockWithVerifyContext
	if err := blk.VerifyWithContext(ctx, proposerVMBlockCtx); err != nil {
		return err
	}
	_, err := b.VM.atomicBackend.InsertTxs(common.Hash(blk.ID()), blk.Height(), common.Hash(blk.Parent()), b.Txs)
	return err
}

func (b *BlockImpl) Verify(ctx context.Context) error {
	// verify UTXOs named in import txs are present in shared memory.
	if err := b.VM.VerifyUTXOsPresent(common.Hash(b.ID()), b.Height(), b.Txs); err != nil {
		return err
	}
	blk := b.BlockWithVerifyContext
	if err := blk.Verify(ctx); err != nil {
		return err
	}
	_, err := b.VM.atomicBackend.InsertTxs(common.Hash(blk.ID()), blk.Height(), common.Hash(blk.Parent()), b.Txs)
	return err
}

func (b *BlockImpl) Reject(ctx context.Context) error {
	for _, tx := range b.Txs {
		b.VM.Mempool().RemoveTx(tx)
		if err := b.VM.Mempool().AddTx(tx); err != nil {
			log.Debug("Failed to re-issue transaction in rejected block", "txID", tx.ID(), "err", err)
		}
	}
	atomicState, err := b.VM.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Reject
		return err
	}
	if err := atomicState.Reject(); err != nil {
		return err
	}
	return b.BlockWithVerifyContext.Reject(ctx)
}
