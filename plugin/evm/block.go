// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
)

// Block implements the snowman.Block interface
type Block struct {
	id       ids.ID
	status   choices.Status
	ethBlock *types.Block
	vm       *VM
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return b.id }

// Accept implements the snowman.Block interface
func (b *Block) Accept() error {
	vm := b.vm
	b.status = choices.Accepted

	log.Trace(fmt.Sprintf("Block %s is accepted", b.ID()))

	tx := vm.extractAtomicTx(b.ethBlock)
	if tx == nil {
		return nil
	}
	utx, ok := tx.UnsignedTx.(UnsignedAtomicTx)
	if !ok {
		return errors.New("unknown tx type")
	}
	txID := tx.ID()
	if err := vm.acceptedAtomicTxDB.Put(txID[:], tx.Bytes()); err != nil {
		return err
	}

	return utx.Accept(vm.ctx, nil)
}

// Reject implements the snowman.Block interface
func (b *Block) Reject() error {
	b.status = choices.Rejected
	log.Trace(fmt.Sprintf("Block %s is rejected", b.ID()))
	return nil
}

// Status implements the snowman.Block interface
// this internal block type is maintained on a best effort
// basis, while the canonical status of blocks is maintained
// by ChainState, which is called by the Consensus Engine.
// Therefore, the Status of an internal Block should not
// be relied upon.
func (b *Block) Status() choices.Status {
	if b.status == choices.Unknown && b.ethBlock != nil {
		return choices.Processing
	}
	return b.status
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() snowman.Block {
	parentID := ids.ID(b.ethBlock.ParentHash())
	if block, err := b.vm.GetBlock(parentID); err == nil {
		return block.(*BlockWrapper).Block
	}
	return &missing.Block{BlkID: parentID}
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.ethBlock.Number().Uint64()
}

// Verify implements the snowman.Block interface
// Verify will only be called on a block once the block and
// its complete ancestry is known
func (b *Block) Verify() error {
	// Only enforce a minimum fee when bootstrapping has finished
	if b.vm.ctx.IsBootstrapped() {
		// Ensure the minimum gas price is paid for every transaction
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(params.MinGasPrice) < 0 {
				return errInvalidBlock
			}
		}
	}

	tx := b.vm.extractAtomicTx(b.ethBlock)
	if tx != nil {
		pState, err := b.vm.chain.BlockState(b.Parent().(*Block).ethBlock)
		if err != nil {
			return err
		}
		switch atx := tx.UnsignedTx.(type) {
		case *UnsignedImportTx:
			lastBlock := b.vm.getLastAcceptedEthBlock()
			// If this block has height less than or equal to the last block
			// verify that it was already accepted.
			// In practice, verify should not be called on an already Accepted
			// block.
			if cmp := b.ethBlock.Number().Cmp(lastBlock.Number()); cmp <= 0 {
				return b.verifyPastBlock()
			}
			p := b.Parent()
			path := []*Block{}
			inputs := ids.Set{}
			for {
				if cmp := p.(*Block).ethBlock.Number().Cmp(lastBlock.Number()); cmp <= 0 {
					break
				}
				if atomicInputs, hit := b.vm.blockAtomicInputCache.Get(p.ID()); hit {
					inputs = atomicInputs.(ids.Set)
					break
				}
				path = append(path, p.(*Block))
				p = p.Parent().(*Block)
			}
			for i := len(path) - 1; i >= 0; i-- {
				inputsCopy := ids.Set{}
				p := path[i]
				atx := b.vm.extractAtomicTx(p.ethBlock)
				if atx != nil {
					inputs.Union(atx.UnsignedTx.(UnsignedAtomicTx).InputUTXOs())
					inputsCopy.Union(inputs)
				}
				b.vm.blockAtomicInputCache.Put(p.ID(), inputsCopy)
			}
			if inputUTXOs := atx.InputUTXOs(); inputUTXOs.Overlaps(inputs) {
				return errInvalidBlock
			}
		case *UnsignedExportTx:
		default:
			return errors.New("unknown atomic tx type")
		}

		utx := tx.UnsignedTx.(UnsignedAtomicTx)
		if err := utx.SemanticVerify(b.vm, tx); err != nil {
			return errInvalidBlock
		}
		bc := b.vm.chain.BlockChain()
		if _, _, _, err = bc.Processor().Process(b.ethBlock, pState, *bc.GetVMConfig()); err != nil {
			return fmt.Errorf("block processing failed: %w", err)
		}
	}
	_, err := b.vm.chain.InsertChain([]*types.Block{b.ethBlock})
	return err
}

func (b *Block) verifyPastBlock() error {
	if b.ethBlock.Hash() == b.vm.genesisHash {
		return nil
	}

	blk, err := b.vm.ChainState.GetBlock(b.ID())
	if err != nil {
		return fmt.Errorf("failed to get past block: %w", err)
	}

	if status := blk.Status(); status == choices.Accepted {
		return nil
	}

	return fmt.Errorf("different block was accepted at the height of block: %s", b.ID())
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
