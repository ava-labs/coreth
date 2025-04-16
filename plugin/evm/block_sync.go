package evm

import (
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/statesync"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
)

const (
	bufferSize = 3 * statesync.PivotInterval
)

type queueElement struct {
	block    *Block
	callback func() error
}

type SyncQueue struct {
	buffer            []*queueElement
	bufferLen         int
	enabled           bool
	ethBlockPivotChan chan *types.Block // Should be passed to sync manager
	blocker           sync.Mutex        // Lock used for synchronization with sync process
}

func NewSyncQueue() *SyncQueue {
	return &SyncQueue{
		buffer:            make([]*queueElement, bufferSize),
		bufferLen:         0,
		ethBlockPivotChan: make(chan *types.Block),
	}
}

// ReceivePivot returns the channel that the sync manager listens to for pivot blocks
// Read only!!!
func (q *SyncQueue) ReceivePivot() chan *types.Block {
	return q.ethBlockPivotChan
}

// Add a block to the queue.
func (q *SyncQueue) addElement(elem *queueElement) {
	q.buffer[q.bufferLen] = elem
	q.bufferLen++
}

// Returns whether the queue should be used for incoming blocks.
// By default, is false.
func (q *SyncQueue) Enabled() bool {
	return q.enabled
}

// Set enabled status of queue.
func (q *SyncQueue) SetEnabled(enabled bool) {
	q.enabled = enabled
}

// Must be called prior to calling any method.
func (q *SyncQueue) Block() {
	q.blocker.Lock()
}

// Allows other threads to proceed.
func (q *SyncQueue) Unblock() {
	q.blocker.Unlock()
}

// Executes all callbacks in the queue.
// This should be called when the sync manager is done.
func (q *SyncQueue) ExecQueue() error {
	defer func() { q.bufferLen = 0 }() // if this fails, state is irreparable

	for i := range q.bufferLen {
		elem := q.buffer[i]
		if err := elem.callback(); err != nil {
			return fmt.Errorf("failed to execute callback on block with height %d: %w", elem.block.Height(), err)
		}
	}

	return nil
}

// Removes elements from the queue with height <= `keepAfter`.
func (q *SyncQueue) clearQueue(keepAfter uint64) {
	newLength := 0

	for i := range q.bufferLen {
		elem := q.buffer[i]
		if elem.block.Height() > keepAfter {
			// Postpone eviction until next pivot
			q.buffer[newLength] = elem // newLength <= i, so this element was already viewed
			newLength++
		}
	}

	q.bufferLen = newLength
}

func (q *SyncQueue) Accept(b *Block) error {
	log.Debug("Accepting block during sync", "hash", b.ID(), "height", b.Height())
	vm := b.vm

	// Although returning an error from Accept is considered fatal, it is good
	// practice to cleanup the batch we were modifying in the case of an error.
	defer vm.versiondb.Abort()

	if err := b.acceptAtomicOps(); err != nil {
		return fmt.Errorf("failed to accept atomic ops during accept: %w", err)
	}

	// Write the canonical hash to the chain database
	batch := vm.chaindb.NewBatch()
	rawdb.WriteCanonicalHash(batch, b.ethBlock.Hash(), b.ethBlock.NumberU64())
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write block during sync: %w", err)
	}

	// Add new block to queue
	q.addElement(&queueElement{
		block:    b,
		callback: func() error { return b.accept() },
	})

	if b.Height()%statesync.PivotInterval == 0 {
		// If this is a pivot block, notify the sync manager
		// If no one is listening, we will not block - could happen if
		// the sync manager is not started yet or it has finisehd
		select {
		case q.ethBlockPivotChan <- b.ethBlock:
		default:
		}

		// Clear the queue of blocks that are no longer needed
		q.clearQueue(b.Height())
	}

	return nil
}

func (q *SyncQueue) Reject(b *Block) error {
	vm := b.vm
	block := b.ethBlock

	// Because this will not be called during bootstrapping, this is guaranteed to be a valid atomic operation
	atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Reject
		panic(fmt.Sprintf("failed to get atomic state for block height %d during sync: %v", b.Height(), err))
	}
	if err := atomicState.Reject(); err != nil {
		return err
	}

	// Remove the block since its data is no longer needed
	batch := vm.chaindb.NewBatch()
	rawdb.DeleteBlock(batch, block.Hash(), block.NumberU64())
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write delete block batch: %w", err)
	}

	// Add new block to queue
	q.addElement(&queueElement{
		block:    b,
		callback: func() error { return b.reject() },
	})

	log.Debug("Returning from reject without error", "block", b.ID(), "height", b.Height())
	return nil
}

func (q *SyncQueue) Verify(b *Block) error {
	log.Debug("Verifying block during sync", "block", b.ID(), "height", b.Height())

	// Emulate vm.onExtraStateChange to only apply atomic operations
	var (
		block      = b.ethBlock
		header     = block.Header()
		vm         = b.vm
		rules      = vm.chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
		rulesExtra = *params.GetRulesExtra(rules)
	)

	_, lastHeight, err := vm.readLastAccepted()
	if err != nil {
		return fmt.Errorf("failed to read last accepted block: %w", err)
	}

	// Used to avoid re-evaluating atomic transactions, since they were executed synchronously
	if b.Height() <= lastHeight {
		currentAtomicBackend := vm.atomicBackend
		vm.atomicBackend = nil
		defer func() { vm.atomicBackend = currentAtomicBackend }()
	}

	// Write the block to the database using chaindb
	batch := vm.chaindb.NewBatch()
	rawdb.WriteBlock(batch, block)
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write block during sync: %w", err)
	}

	// If [atomicBackend] is nil, the VM is still initializing and is reprocessing accepted blocks.
	if vm.atomicBackend != nil {
		// verify UTXOs named in import txs are present in shared memory.
		if err := b.verifyUTXOsPresent(); err != nil {
			return err
		}

		txs, err := atomic.ExtractAtomicTxs(customtypes.BlockExtData(block), rulesExtra.IsApricotPhase5, atomic.Codec)
		if err != nil {
			return err
		}
		if vm.atomicBackend.IsBonus(block.NumberU64(), block.Hash()) {
			log.Info("skipping atomic tx verification on bonus block", "block", block.Hash())
		} else {
			// Verify [txs] do not conflict with themselves or ancestor blocks.
			if err := vm.verifyTxs(txs, block.ParentHash(), block.BaseFee(), block.NumberU64(), rulesExtra); err != nil {
				return err
			}
		}
		// Update the atomic backend with [txs] from this block.
		//
		// Note: The atomic trie canonically contains the duplicate operations
		// from any bonus blocks.
		_, err = vm.atomicBackend.InsertTxs(block.Hash(), block.NumberU64(), block.ParentHash(), txs)
		if err != nil {
			return err
		}
	}

	// Add new block to queue
	q.addElement(&queueElement{
		block:    b,
		callback: func() error { return b.verify(true) },
	})

	return nil
}
