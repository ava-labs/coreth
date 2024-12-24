package evm

import (
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
)

// Each 1000 blocks, we will make a new file.
// Each file contains:
// - Block number (8 bytes)
// - Block hash (32 bytes)
// - Transactions (uint16)
// - Atomic transactions (uint16)
// - Accounts Read (uint16)
// - Storages Read (uint16)
// - Accounts Written (uint16)
// - Storages Written (uint16)
// For each account read:
// - Account address hash (32 bytes)
// - Value len (byte)
// - Value (variable)
// For each storage read:
// - Account address hash (32 bytes)
// - Key hash (32 bytes)
// - Value len (byte)
// - Value (variable)
// For each account written:
// - Account address hash (32 bytes)
// - Value len (byte)
// - Value (variable)
// For each storage written:
// - Account address hash (32 bytes)
// - Key hash (32 bytes)
// - Value len (byte)

type blockRecorder struct {
	accountReads  []triedb.KV
	accountWrites []triedb.KV
	storageReads  []triedb.KV
	storageWrites []triedb.KV
}

func (b *blockRecorder) MustUpdate(key, value []byte) {
	switch len(key) {
	case 32:
		b.accountWrites = append(b.accountWrites, triedb.KV{Key: key, Value: value})
	case 64:
		b.storageWrites = append(b.storageWrites, triedb.KV{Key: key, Value: value})
	default:
		panic("unexpected key length")
	}
}

func (b *blockRecorder) RecordAccountRead(key common.Hash, value []byte) error {
	b.accountReads = append(b.accountReads, triedb.KV{Key: key[:], Value: value})
	return nil
}

func (b *blockRecorder) RecordStorageRead(account common.Hash, key common.Hash, value []byte) error {
	b.storageReads = append(b.storageReads, triedb.KV{Key: append(account[:], key[:]...), Value: value})
	return nil
}

func (b *blockRecorder) Summary(block *types.Block, atomicTxs uint16) {
	fmt.Printf("Block %d: %s (%d txs + %d atomic)\tReads: %d(a) %d(s), Writes: %d(a) %d(s)\n",
		block.NumberU64(),
		block.Hash().TerminalString(),
		len(block.Transactions()),
		atomicTxs,
		len(b.accountReads),
		len(b.storageReads),
		len(b.accountWrites),
		len(b.storageWrites),
	)

	if !tapeVerbose {
		return
	}
	fmt.Printf("Account Reads: %d\n", len(b.accountReads))
	for _, kv := range b.accountReads {
		fmt.Printf("  %x: %x\n", kv.Key, kv.Value)
	}
	fmt.Printf("Storage Reads: %d\n", len(b.storageReads))
	for _, kv := range b.storageReads {
		fmt.Printf("  %x: %x\n", kv.Key, kv.Value)
	}

	fmt.Printf("Account Writes: %d\n", len(b.accountWrites))
	for _, kv := range b.accountWrites {
		fmt.Printf("  %x: %x\n", kv.Key, kv.Value)
	}

	fmt.Printf("Storage Writes: %d\n", len(b.storageWrites))
	for _, kv := range b.storageWrites {
		fmt.Printf("  %x: %x\n", kv.Key, kv.Value)
	}
}

func (b *blockRecorder) Reset() {
	b.accountReads = nil
	b.accountWrites = nil
	b.storageReads = nil
	b.storageWrites = nil
}
