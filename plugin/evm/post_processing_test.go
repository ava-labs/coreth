package evm

import (
	"io"
	"testing"

	"github.com/Yiling-J/theine-go"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/stretchr/testify/require"
)

type totals struct {
	blocks         uint64
	txs            uint64
	atomicTxs      uint64
	accountReads   uint64
	storageReads   uint64
	accountWrites  uint64
	storageWrites  uint64
	accountUpdates uint64
	storageUpdates uint64
	accountDeletes uint64
	storageDeletes uint64
	accounts       uint64
	storage        uint64

	// cache stats
	accountReadHits uint64
	storageReadHits uint64
}

func TestPostProcess(t *testing.T) {
	if tapeDir == "" {
		t.Skip("No tape directory provided")
	}
	start, end := startBlock, endBlock
	if start == 0 {
		start = 1 // TODO: Verify whether genesis outs were recorded in the first block
	}

	cache, err := theine.NewBuilder[string, []byte](readCacheSize * units.MiB).Build()
	require.NoError(t, err)

	cacheFn := func(k string, v []byte) bool {
		_, found := cache.Get(k)
		cache.Set(k, v, int64(len(k)+len(v)))
		return found
	}

	var sum totals
	fm := &fileManager{dir: tapeDir, newEach: 10_000}

	var lastReported totals
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

		tapeResult := &tapeResult{
			accountReads: make(map[string][]byte),
			storageReads: make(map[string][]byte),
		}
		tapeTxs := processTape(t, r, tapeResult, cacheFn, &sum)
		require.Equal(t, txs, tapeTxs)

		accountWrites, err := readUint16(r)
		require.NoError(t, err)

		storageWrites, err := readUint16(r)
		require.NoError(t, err)

		accountUpdates, storageUpdates := 0, 0
		accountDeletes, storageDeletes := 0, 0

		for j := 0; j < int(accountWrites); j++ {
			k, v, err := readKV(r, 32)
			require.NoError(t, err)
			if prev, ok := tapeResult.accountReads[string(k)]; ok {
				if len(prev) > 0 && len(v) == 0 {
					accountDeletes++
				} else if len(prev) > 0 {
					accountUpdates++
				}
			} else {
				t.Logf("account write without read: %x -> %x", k, v)
			}
			if tapeVerbose {
				t.Logf("account write: %x -> %x", k, v)
			}
		}
		for j := 0; j < int(storageWrites); j++ {
			k, v, err := readKV(r, 64)
			require.NoError(t, err)
			if prev, ok := tapeResult.storageReads[string(k)]; ok {
				if len(prev) > 0 && len(v) == 0 {
					storageDeletes++
				} else if len(prev) > 0 {
					storageUpdates++
				}
			} else {
				t.Logf("storage write without read: %x -> %x", k, v)
			}
			if tapeVerbose {
				t.Logf("storage write: %x -> %x", k, v)
			}
		}

		sum.blocks++
		sum.txs += uint64(txs)
		sum.atomicTxs += uint64(atomicTxs)
		sum.accountReads += uint64(len(tapeResult.accountReads))
		sum.storageReads += uint64(len(tapeResult.storageReads))
		sum.accountWrites += uint64(accountWrites)
		sum.storageWrites += uint64(storageWrites)
		sum.accountUpdates += uint64(accountUpdates)
		sum.storageUpdates += uint64(storageUpdates)
		sum.accountDeletes += uint64(accountDeletes)
		sum.storageDeletes += uint64(storageDeletes)
		sum.accounts += uint64(int(accountWrites) - accountUpdates - 2*accountDeletes)
		sum.storage += uint64(int(storageWrites) - storageUpdates - 2*storageDeletes)

		if blockNumber%uint64(logEach) == 0 {
			t.Logf("Block[%s]: (%d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d)",
				blockHash.TerminalString(), blockNumber,
				sum.txs, sum.atomicTxs,
				sum.accountReads, sum.storageReads,
				sum.accountReadHits, sum.storageReadHits,
				sum.accountWrites, sum.storageWrites,
				sum.accountUpdates, sum.storageUpdates, sum.accountDeletes, sum.storageDeletes,
				sum.accounts, sum.storage)
			hits := sum.accountReadHits - lastReported.accountReadHits + sum.storageReadHits - lastReported.storageReadHits
			total := sum.accountReads - lastReported.accountReads + sum.storageReads - lastReported.storageReads
			t.Logf(
				"Cache stats: %d hits, %d misses, %.2f hit rate, %d entries (= %.4f of state), %d MiB",
				hits, total-hits, float64(hits)/float64(total),
				cache.Len(), float64(cache.Len())/float64(sum.accounts+sum.storage),
				cache.EstimatedSize()/(units.MiB),
			)
			lastReported = sum
		}
	}
}

type tapeResult struct {
	accountReads, storageReads map[string][]byte
}

// cache should return true if the value was found in the cache
func processTape(t *testing.T, r io.Reader, tapeResult *tapeResult, cache func(k string, v []byte) bool, sum *totals) uint16 {
	length, err := readUint32(r)
	require.NoError(t, err)

	pos := 0
	txCount := uint16(0)
	for pos < int(length) {
		typ, err := readByte(r)
		require.NoError(t, err)
		pos++

		switch typ {
		case typeAccount:
			key, val, err := readKV(r, 32)
			require.NoError(t, err)
			pos += 32 + 1 + len(val)
			k := string(key)
			if _, ok := tapeResult.accountReads[k]; !ok {
				tapeResult.accountReads[k] = val
				if cache(k, val) {
					sum.accountReadHits++
				}
			}
		case typeStorage:
			key, val, err := readKV(r, 64)
			require.NoError(t, err)
			pos += 64 + 1 + len(val)
			k := string(key)
			if _, ok := tapeResult.storageReads[k]; !ok {
				tapeResult.storageReads[k] = val
				if cache(k, val) {
					sum.storageReadHits++
				}
			}
		case typeEndTx:
			txCount++
		}
	}
	return txCount
}
