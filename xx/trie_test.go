package test

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
	"time"

	avagoDb "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/units"
	xmerkledb "github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"
)

var (
	// merkledb options
	merkleDBBranchFactor          = 16
	valueNodeCacheSizeMB          = 256
	intermediateNodeCacheSizeMB   = 256
	intermediateWriteBufferSizeKB = 1024
	intermediateWriteBatchSizeKB  = 256
)

func getMerkleDB(t testing.TB, mdbKVStore avagoDb.Database) xmerkledb.MerkleDB {
	ctx := context.Background()
	mdb, err := xmerkledb.New(ctx, mdbKVStore, xmerkledb.Config{
		BranchFactor:                xmerkledb.BranchFactor(merkleDBBranchFactor),
		Hasher:                      xmerkledb.DefaultHasher,
		HistoryLength:               1,
		RootGenConcurrency:          0,
		ValueNodeCacheSize:          uint(valueNodeCacheSizeMB) * units.MiB,
		IntermediateNodeCacheSize:   uint(intermediateNodeCacheSizeMB) * units.MiB,
		IntermediateWriteBufferSize: uint(intermediateWriteBufferSizeKB) * units.KiB,
		IntermediateWriteBatchSize:  uint(intermediateWriteBatchSizeKB) * units.KiB,
		Reg:                         prometheus.NewRegistry(),
		TraceLevel:                  xmerkledb.InfoTrace,
		Tracer:                      trace.Noop,
	})
	require.NoError(t, err)

	return mdb
}

func BenchmarkTrie(b *testing.B) {
	disk := rawdb.NewMemoryDatabase()
	config := *pathdb.Defaults
	config.CleanCacheSize = 0
	tdb := triedb.NewDatabase(disk, &triedb.Config{PathDB: &config})
	var lastKey uint64

	for _, initialSize := range []uint64{1_000_000, 4_000_000, 16_000_000, 32_000_000, 64_000_000} {
		getKV := func(i uint64) (key, value common.Hash) {
			key = common.BytesToHash(hashKey(binary.BigEndian.AppendUint64(nil, i)))
			value = common.BytesToHash(binary.BigEndian.AppendUint64(nil, i))
			return
		}

		tr, err := trie.New(trie.TrieID(types.EmptyRootHash), tdb)
		require.NoError(b, err)

		var lastRoot common.Hash
		commit := func() {
			root, set, err := tr.Commit(false)
			require.NoError(b, err)
			tdb.Update(root, lastRoot, 0, trienode.NewWithNodeSet(set), nil)
			require.NoError(b, err)
			require.NoError(b, tdb.Commit(root, false))
			lastRoot = root
			tr, err = trie.New(trie.TrieID(root), tdb)
			require.NoError(b, err)
		}

		batchSize := uint64(100_000)
		lastTime := time.Now()
		for i := lastKey; i < initialSize; i++ {
			key, value := getKV(i)
			tr.MustUpdate(key[:], value[:])

			if (i+1)%batchSize == 0 {
				commit()
				b.Logf("Committed %d(k), root: %s, time: %v", (i+1)/1000, lastRoot.TerminalString(), time.Since(lastTime).Truncate(time.Millisecond))
				if (i+1)%(batchSize*10) == 0 {
					logMemUse(b, tdb)
				}
				lastTime = time.Now()
			}
		}
		lastKey = initialSize

		for _, writeBatch := range []uint64{200} {
			b.Run(fmt.Sprintf("InitialSize-%d_WriteBatch-%d", initialSize, writeBatch), func(b *testing.B) {
				for i := uint64(0); i < uint64(b.N); i++ {
					for j := uint64(0); j < writeBatch; j++ {
						b.StopTimer()
						key, value := getKV(lastKey + j)
						b.StartTimer()
						tr.MustUpdate(key[:], value[:])
					}
					commit()
					lastKey += writeBatch
				}
				logMemUse(b, tdb)
			})
		}
	}
}

var sha = sha3.NewLegacyKeccak256()

func hashKey(key []byte) []byte {
	sha.Reset()
	sha.Write(key)
	return sha.Sum(nil)
}

var global xmerkledb.MerkleDB

func BenchmarkTrieMerkleDB(b *testing.B) {
	db := memdb.New()
	mdb := getMerkleDB(b, db)
	global = mdb
	var lastKey uint64

	getKV := func(i uint64) (key, value common.Hash) {
		key = common.BytesToHash(hashKey(binary.BigEndian.AppendUint64(nil, i)))
		value = common.BytesToHash(binary.BigEndian.AppendUint64(nil, i))
		return
	}

	for _, initialSize := range []uint64{1_000_000, 4_000_000} {
		var lastRoot common.Hash
		batchSize := uint64(100_000)
		batch := xmerkledb.ViewChanges{
			ConsumeBytes: true,
			BatchOps:     make([]avagoDb.BatchOp, 0, batchSize),
		}
		commit := func() {
			v, err := mdb.NewView(context.Background(), batch)
			require.NoError(b, err)
			require.NoError(b, v.CommitToDB(context.Background()))
			rootId, err := mdb.GetMerkleRoot(context.Background())
			require.NoError(b, err)
			lastRoot = common.Hash(rootId)
			// Reuse BatchOps to avoid allocations
			batch.BatchOps = batch.BatchOps[:0]
		}

		lastTime := time.Now()
		for i := lastKey; i < initialSize; i++ {
			k, v := getKV(i)
			batch.BatchOps = append(batch.BatchOps, avagoDb.BatchOp{
				Key:   k[:],
				Value: v[:],
			})

			if (i+1)%batchSize == 0 {
				commit()
				b.Logf("Committed %d(k), root: %s, time: %v", (i+1)/1000, lastRoot.TerminalString(), time.Since(lastTime).Truncate(time.Millisecond))
				if (i+1)%(batchSize*10) == 0 {
					logMemUse(b, mdb)
				}
				lastTime = time.Now()
			}
		}
		lastKey = initialSize

		for _, writeBatch := range []uint64{200} {
			b.Run(fmt.Sprintf("InitialSize-%d_WriteBatch-%d", initialSize, writeBatch), func(b *testing.B) {
				for i := uint64(0); i < uint64(b.N); i++ {
					for j := uint64(0); j < writeBatch; j++ {
						b.StopTimer()
						k, v := getKV(lastKey + j)
						b.StartTimer()
						batch.BatchOps = append(batch.BatchOps, avagoDb.BatchOp{
							Key:   k[:],
							Value: v[:],
						})
					}
					commit()
					lastKey += writeBatch
				}
				logMemUse(b, mdb)
			})
		}
	}
}

func logMemUse(b *testing.B, tdb any) {
	b.StopTimer()
	runtime.GC()
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	b.Logf("HeapInuse: %dMBs (db ref: %p)", memStats.HeapInuse/1024/1024, tdb)
	b.StartTimer()
}
