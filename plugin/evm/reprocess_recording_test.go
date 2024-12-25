package evm

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
)

// Each 10000 blocks, we will make a new file.
// Each file contains:
// - Block number (8 bytes)
// - Block hash (32 bytes)
// - Transactions (uint16)
// - Atomic transactions (uint16)
// - Length of read tape (uint32)
// - Read tape (variable)
//   - byte type (1 byte)
//   - type = account
//     - Account address hash (32 bytes)
//     - Value len (byte)
//     - Value (variable)
//   - type = storage
//     - Account address hash (32 bytes)
//     - Key hash (32 bytes)
//     - Value len (byte)
//     - Value (variable)
//   - type = end tx
// - Accounts Written (uint16)
// - Storages Written (uint16)
// For each account written:
// - Account address hash (32 bytes)
// - Value len (byte)
// - Value (variable)
// For each storage written:
// - Account address hash (32 bytes)
// - Key hash (32 bytes)
// - Value len (byte)

type blockRecorder struct {
	accountReads int
	storageReads int
	txEnds       int
	readTape     []byte

	accountWrites []triedb.KV
	storageWrites []triedb.KV

	fileManager *fileManager
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

const typeAccount = 0
const typeStorage = 1
const typeEndTx = 2

func (b *blockRecorder) RecordAccountRead(key common.Hash, value []byte) error {
	b.accountReads++
	b.readTape = append(b.readTape, typeAccount)
	b.readTape = append(b.readTape, key[:]...)
	b.readTape = append(b.readTape, byte(len(value)))
	b.readTape = append(b.readTape, value...)
	return nil
}

func (b *blockRecorder) RecordStorageRead(account common.Hash, key common.Hash, value []byte) error {
	b.storageReads++
	b.readTape = append(b.readTape, typeStorage)
	b.readTape = append(b.readTape, account[:]...)
	b.readTape = append(b.readTape, key[:]...)
	b.readTape = append(b.readTape, byte(len(value)))
	b.readTape = append(b.readTape, value...)
	return nil
}

func (b *blockRecorder) RecordTransactionEnd() error {
	b.txEnds++
	b.readTape = append(b.readTape, typeEndTx)
	return nil
}

func (b *blockRecorder) WriteToDisk(block *types.Block, atomicTxs uint16) {
	if b.fileManager == nil {
		return
	}
	w := b.fileManager.GetWriterFor(block.NumberU64())
	if b.Write(block, atomicTxs, w) != nil {
		panic("failed to write")
	}
}

func (b *blockRecorder) Close() {
	if b.fileManager == nil {
		return
	}
	b.fileManager.Close()
}

func (b *blockRecorder) Summary(block *types.Block, atomicTxs uint16) {
	fmt.Printf("Block %d: %s (%d txs + %d atomic)\tReads (acc, storage, tape KBs): %d, %d, %d\t Writes: %d, %d\n",
		block.NumberU64(),
		block.Hash().TerminalString(),
		len(block.Transactions()),
		atomicTxs,
		b.accountReads,
		b.storageReads,
		len(b.readTape)/1024,
		len(b.accountWrites),
		len(b.storageWrites),
	)

	if !tapeVerbose {
		return
	}
	fmt.Printf("Read Tape: %x\n", b.readTape)

	fmt.Printf("Account Writes: %d\n", len(b.accountWrites))
	for _, kv := range b.accountWrites {
		fmt.Printf("  %x: %x\n", kv.Key, kv.Value)
	}

	fmt.Printf("Storage Writes: %d\n", len(b.storageWrites))
	for _, kv := range b.storageWrites {
		fmt.Printf("  %x: %x\n", kv.Key, kv.Value)
	}
}

func writeByte(w io.Writer, b byte) error {
	_, err := w.Write([]byte{b})
	return err
}

func writeUint16(w io.Writer, i uint16) error {
	_, err := w.Write(binary.BigEndian.AppendUint16(nil, i))
	return err
}

func writeUint32(w io.Writer, i uint32) error {
	_, err := w.Write(binary.BigEndian.AppendUint32(nil, i))
	return err
}

func writeUint64(w io.Writer, i uint64) error {
	_, err := w.Write(binary.BigEndian.AppendUint64(nil, i))
	return err
}

func (b *blockRecorder) Write(block *types.Block, atomicTxs uint16, w io.Writer) error {
	if len(block.Transactions()) != b.txEnds {
		panic(fmt.Sprintf("mismatch block txs: %d, ends recorded: %d", len(block.Transactions()), b.txEnds))
	}
	if err := writeUint64(w, block.NumberU64()); err != nil {
		return err
	}
	if _, err := w.Write(block.Hash().Bytes()); err != nil {
		return err
	}
	if err := writeUint16(w, uint16(len(block.Transactions()))); err != nil {
		return err
	}
	if err := writeUint16(w, atomicTxs); err != nil {
		return err
	}
	if err := writeUint32(w, uint32(len(b.readTape))); err != nil {
		return err
	}
	if _, err := w.Write(b.readTape); err != nil {
		return err
	}
	if err := writeUint16(w, uint16(len(b.accountWrites))); err != nil {
		return err
	}
	if err := writeUint16(w, uint16(len(b.storageWrites))); err != nil {
		return err
	}

	for _, kv := range b.accountWrites {
		if _, err := w.Write(kv.Key); err != nil {
			return err
		}
		if err := writeByte(w, byte(len(kv.Value))); err != nil {
			return err
		}
		if _, err := w.Write(kv.Value); err != nil {
			return err
		}
	}

	for _, kv := range b.storageWrites {
		if _, err := w.Write(kv.Key); err != nil {
			return err
		}
		if err := writeByte(w, byte(len(kv.Value))); err != nil {
			return err
		}
		if _, err := w.Write(kv.Value); err != nil {
			return err
		}
	}
	return nil
}

func (b *blockRecorder) Reset() {
	b.readTape = nil
	b.accountWrites = nil
	b.storageWrites = nil
	b.storageReads = 0
	b.accountReads = 0
	b.txEnds = 0
}

type fileManager struct {
	dir      string
	newEach  uint64
	lastFile uint64
	f        *os.File
}

func (f *fileManager) GetWriterFor(blockNumber uint64) io.Writer {
	group := blockNumber - blockNumber%f.newEach
	if group == f.lastFile && f.f != nil {
		return f.f
	}
	if f.f != nil {
		f.f.Close()
	}
	file, err := os.OpenFile(fmt.Sprintf("%s/%08d", f.dir, group), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	f.f = file
	return f.f
}

func (f *fileManager) Close() error {
	if f.f != nil {
		return f.f.Close()
	}
	return nil
}
