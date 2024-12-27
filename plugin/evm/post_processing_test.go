package evm

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type totals struct {
	blocks        uint64
	txs           uint64
	atomicTxs     uint64
	accountReads  uint64
	storageReads  uint64
	accountWrites uint64
	storageWrites uint64
}

func TestPostProcess(t *testing.T) {
	start, end := startBlock, endBlock
	if start == 0 {
		start = 1 // TODO: Verify whether genesis outs were recorded in the first block
	}

	var totals totals
	fm := &fileManager{dir: tapeDir, newEach: 10_000}
	for i := start; i <= end; i++ {
		r := fm.GetReaderFor(i)

		blockNumber, err := readUint64(r)
		require.NoError(t, err)
		require.Equal(t, i, blockNumber)

		blockHash, err := readHash(r)
		require.NoError(t, err)

		txs, err := readUint16(r)
		require.NoError(t, err)

		atomicTxs, err := readUint16(r)
		require.NoError(t, err)

		tapeTxs, accountReads, storageReads := processTape(t, r)
		require.Equal(t, txs, tapeTxs)

		accountWrites, err := readUint16(r)
		require.NoError(t, err)

		storageWrites, err := readUint16(r)
		require.NoError(t, err)

		for j := 0; j < int(accountWrites); j++ {
			k, v, err := readKV(r, 32)
			require.NoError(t, err)
			if tapeVerbose {
				t.Logf("account write: %x -> %x", k, v)
			}
		}
		for j := 0; j < int(storageWrites); j++ {
			k, v, err := readKV(r, 64)
			require.NoError(t, err)
			if tapeVerbose {
				t.Logf("storage write: %x -> %x", k, v)
			}
		}

		totals.blocks++
		totals.txs += uint64(txs)
		totals.atomicTxs += uint64(atomicTxs)
		totals.accountReads += uint64(accountReads)
		totals.storageReads += uint64(storageReads)
		totals.accountWrites += uint64(accountWrites)
		totals.storageWrites += uint64(storageWrites)

		t.Logf("Block[%d:%s]: totals: (txs: %d, atomic: %d, accReads: %d, storageReads: %d, accWrites: %d, storageWrites: %d)",
			blockNumber, blockHash.TerminalString(), totals.txs, totals.atomicTxs,
			totals.accountReads, totals.storageReads, totals.accountWrites, totals.storageWrites)
	}
}

func processTape(t *testing.T, r io.Reader) (uint16, int, int) {
	length, err := readUint32(r)
	require.NoError(t, err)

	pos := 0
	txCount := uint16(0)
	accountReads, storageReads := 0, 0
	for pos < int(length) {
		typ, err := readByte(r)
		require.NoError(t, err)
		pos++

		switch typ {
		case typeAccount:
			_, val, err := readKV(r, 32)
			require.NoError(t, err)
			pos += 32 + 1 + len(val)
			accountReads++
		case typeStorage:
			_, val, err := readKV(r, 64)
			require.NoError(t, err)
			pos += 64 + 1 + len(val)
			storageReads++
		case typeEndTx:
			txCount++
		}
	}
	return txCount, accountReads, storageReads
}
