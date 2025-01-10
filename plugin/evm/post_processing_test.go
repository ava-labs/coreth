package evm

import (
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/Yiling-J/theine-go"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/shim/legacy"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/maypok86/otter"
	"github.com/stretchr/testify/require"
	"github.com/valyala/histogram"
	"golang.org/x/crypto/sha3"
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

	// These are int64 as we want to compute the difference (since last log),
	// and state may be deleted.
	accounts int64
	storage  int64

	// cache stats
	accountReadHits        uint64
	storageReadHits        uint64
	accountWriteHits       uint64
	storageWriteHits       uint64
	writeCacheEvictAccount uint64
	writeCacheEvictStorage uint64

	// eviction (historical state storage update) time
	writeCacheEvictTime time.Duration

	// update time (historical state state commitment + persistence)
	storageUpdateTime   time.Duration
	storageUpdateCount  uint64
	storagePersistTime  time.Duration
	storagePersistCount uint64
}

type cacheIntf interface {
	GetAndSet(k string, v []byte) bool
	Delete(k string)
	Len() int
	EstimatedSize() int
}

type fastCache struct {
	cache *fastcache.Cache
}

func (c *fastCache) GetAndSet(k string, v []byte) bool {
	found := c.cache.Has([]byte(k))
	c.cache.Set([]byte(k), v)
	return found
}

func (c *fastCache) Len() int {
	var stats fastcache.Stats
	c.cache.UpdateStats(&stats)
	return int(stats.EntriesCount)
}

func (c *fastCache) EstimatedSize() int {
	var stats fastcache.Stats
	c.cache.UpdateStats(&stats)
	return int(stats.BytesSize)
}

func (c *fastCache) Delete(k string) {
	c.cache.Del([]byte(k))
}

type theineCache struct {
	*theine.Cache[string, []byte]
}

func (c *theineCache) GetAndSet(k string, v []byte) bool {
	_, found := c.Cache.Get(k)
	c.Cache.Set(k, v, int64(len(k)+len(v)))
	return found
}

type otterCache struct {
	otter.Cache[string, []byte]
}

func (c *otterCache) GetAndSet(k string, v []byte) bool {
	_, found := c.Cache.Get(k)
	c.Cache.Set(k, v)
	return found
}

func (c *otterCache) EstimatedSize() int {
	return c.Cache.Capacity()
}

func (c *otterCache) Len() int {
	return c.Cache.Size()
}

type noCache[K, V any] struct {
	onEvict func(k K, v V)
}

func (c *noCache[K, V]) Get(k K) (v V, ok bool)         { return }
func (c *noCache[K, V]) GetAndSet(k K, v V) bool        { return false }
func (c *noCache[K, V]) Delete(k K)                     {}
func (c *noCache[K, V]) Len() int                       { return 0 }
func (c *noCache[K, V]) EstimatedSize() int             { return 0 }
func (c *noCache[K, V]) GetOldest() (k K, v V, ok bool) { return }

func (c *noCache[K, V]) Add(k K, v V) bool {
	if c.onEvict != nil {
		c.onEvict(k, v)
	}
	return false
}

type withUpdatedAt struct {
	val       []byte
	updatedAt uint64
}

type writeCache[K, V any] interface {
	Get(k K) (V, bool)
	Add(k K, v V) bool
	GetOldest() (K, V, bool)
	Len() int
}

func TestPostProcess(t *testing.T) {
	if tapeDir == "" {
		t.Skip("No tape directory provided")
	}
	start, end := startBlock, endBlock
	if start == 0 {
		start = 1 // TODO: Verify whether genesis outs were recorded in the first block
	}

	var cache cacheIntf
	cacheBytes := readCacheSize * units.MiB
	if readCacheBackend == "fastcache" {
		cache = &fastCache{cache: fastcache.New(int(cacheBytes))}
	} else if readCacheBackend == "theine" {
		impl, err := theine.NewBuilder[string, []byte](cacheBytes).Build()
		require.NoError(t, err)
		cache = &theineCache{Cache: impl}
	} else if readCacheBackend == "otter" {
		impl, err := otter.MustBuilder[string, []byte](int(cacheBytes)).
			CollectStats().
			Cost(func(key string, value []byte) uint32 {
				return uint32(len(key) + len(value))
			}).Build()
		if err != nil {
			panic(err)
		}
		cache = &otterCache{Cache: impl}
	} else if readCacheBackend == "none" {
		cache = &noCache[string, []byte]{}
	} else {
		t.Fatalf("Unknown cache backend: %s", readCacheBackend)
	}

	var (
		dbs          dbs
		sourceDb     ethdb.Database
		sum          totals
		blockNumber  uint64
		storageRoot  common.Hash
		storage      triedb.KVBackend
		evitcedBatch triedb.Batch
		lastCommit   struct {
			txs    uint64
			number uint64
		}
		commitLock sync.Mutex
	)
	if sourceDbDir != "" {
		sourceDb = openSourceDB(t)
		defer sourceDb.Close()
	}

	if storageBackend != "none" {
		dbs = openDBs(t)
		defer dbs.Close()
		CleanupOnInterrupt(func() {
			commitLock.Lock()
			dbs.Close()
			commitLock.Unlock()
		})

		lastHash, lastRoot, lastHeight := getMetadata(dbs.metadata)
		t.Logf("Persisted metadata: Last hash: %x, Last root: %x, Last height: %d", lastHash, lastRoot, lastHeight)
		lastCommit.number = lastHeight

		if usePersistedStartBlock {
			start = lastHeight + 1
		}
		require.Equal(t, lastHeight+1, start, "Last height does not match start block")

		storage = getKVBackend(t, storageBackend, dbs.merkledb)
		if storageBackend == "legacy" {
			cacheConfig := getCacheConfig(t, storageBackend, storage)
			tdbConfig := cacheConfig.TrieDBConfig()
			tdb := triedb.NewDatabase(dbs.chain, tdbConfig)
			storage = legacy.New(tdb, lastRoot, lastHeight, true)
		}
		require.Equal(t, lastRoot, storage.Root(), "Root mismatch")
		storageRoot = lastRoot
		t.Logf("Storage backend initialized: %s", storageBackend)
	}

	hst := histogram.NewFast()
	hstWithReset := histogram.NewFast()
	inf := float64(1_000_000_000)
	onEvict := func(k string, v withUpdatedAt) {
		now := time.Now()
		if len(k) == 32 {
			sum.writeCacheEvictAccount++
		} else {
			sum.writeCacheEvictStorage++
		}
		// t.Logf("evicting key: %x @ block %d, updatedAt: %d (%d blocks ago)", short(k), blockNumber, v.updatedAt, blockNumber-v.updatedAt)
		if storage != nil {
			evitcedBatch = append(evitcedBatch, triedb.KV{Key: []byte(k), Value: v.val})
		}
		sum.writeCacheEvictTime += time.Since(now)
	}

	var writeCache writeCache[string, withUpdatedAt] = &noCache[string, withUpdatedAt]{
		onEvict: onEvict,
	}
	if writeCacheSize > 0 {
		var err error
		writeCache, err = lru.NewWithEvict(int(writeCacheSize), onEvict)
		require.NoError(t, err)
	}

	fm := &fileManager{dir: tapeDir, newEach: 10_000}

	var lastReported totals
	for i := start; i <= end; i++ {
		r := fm.GetReaderFor(i)

		var err error
		blockNumber, err = readUint64(r)
		require.NoError(t, err)
		require.LessOrEqual(t, blockNumber, i)

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
		tapeTxs := processTape(t, r, tapeResult, cache.GetAndSet, &sum)
		require.Equal(t, txs, tapeTxs)

		accountWrites, err := readUint16(r)
		require.NoError(t, err)

		storageWrites, err := readUint16(r)
		require.NoError(t, err)

		if blockNumber < i {
			// we need to just finish reading the block but not process it
			for j := 0; j < int(accountWrites); j++ {
				_, _, err := readKV(r, 32)
				require.NoError(t, err)
			}
			for j := 0; j < int(storageWrites); j++ {
				_, _, err := readKV(r, 64)
				require.NoError(t, err)
			}
			if blockNumber%uint64(logEach) == 0 {
				t.Logf("Skipping block %d", blockNumber)
			}
			i--
			continue
		}

		accountUpdates, storageUpdates := 0, 0
		accountDeletes, storageDeletes := 0, 0

		// 1. Read account writes from the tape as they come first
		accountWritesBatch := make([]triedb.KV, accountWrites)
		for j := 0; j < int(accountWrites); j++ {
			k, v, err := readKV(r, 32)
			require.NoError(t, err)
			accountWritesBatch[j] = triedb.KV{Key: k, Value: v}
		}

		// 2. Process storage writes
		for j := 0; j < int(storageWrites); j++ {
			k, v, err := readKV(r, 64)
			require.NoError(t, err)
			if prev, ok := tapeResult.storageReads[string(k)]; ok {
				if len(prev) > 0 && len(v) == 0 {
					storageDeletes++
				} else if len(prev) > 0 || (len(prev) == 0 && len(v) == 0) {
					storageUpdates++
				}
			} else if tapeVerbose {
				t.Logf("storage write without read: %x -> %x", k, v)
			}
			got, found := writeCache.Get(string(k))
			if found {
				hst.Update(float64(blockNumber - got.updatedAt))
				hstWithReset.Update(float64(blockNumber - got.updatedAt))
			} else {
				hst.Update(inf)
				hstWithReset.Update(inf)
			}
			writeCache.Add(string(k), withUpdatedAt{val: v, updatedAt: blockNumber})
			if found {
				sum.storageWriteHits++
			}

			if tapeVerbose {
				t.Logf("storage write: %x -> %x", k, v)
			}
		}

		// 3. Process account writes
		for j := 0; j < int(accountWrites); j++ {
			k, v := accountWritesBatch[j].Key, accountWritesBatch[j].Value
			if prev, ok := tapeResult.accountReads[string(k)]; ok {
				if len(prev) > 0 && len(v) == 0 {
					accountDeletes++
				} else if len(prev) > 0 || (len(prev) == 0 && len(v) == 0) {
					accountUpdates++
				}
			} else if tapeVerbose {
				t.Logf("account write without read: %x -> %x", k, v)
			}
			got, found := writeCache.Get(string(k))
			if found {
				hst.Update(float64(blockNumber - got.updatedAt))
				hstWithReset.Update(float64(blockNumber - got.updatedAt))
			} else {
				hst.Update(inf)
				hstWithReset.Update(inf)
			}
			writeCache.Add(string(k), withUpdatedAt{val: v, updatedAt: blockNumber})
			if found {
				sum.accountWriteHits++
			}

			if tapeVerbose {
				t.Logf("account write: %x -> %x", k, v)
			}
		}

		sum.blocks++
		sum.txs += uint64(txs)
		sum.atomicTxs += uint64(atomicTxs)

		if storage != nil {
			commitLock.Lock()

			shouldCommitBlocks := commitEachBlocks > 0 && blockNumber-lastCommit.number >= uint64(commitEachBlocks)
			shouldCommitTxs := commitEachTxs > 0 && sum.txs+sum.atomicTxs-lastCommit.txs >= uint64(commitEachTxs)
			if len(evitcedBatch) > 0 && (shouldCommitBlocks || shouldCommitTxs) {
				if tapeVerbose {
					for _, kv := range evitcedBatch {
						t.Logf("storing: %x -> %x", kv.Key, kv.Value)
					}
				}
				now := time.Now()
				// Get state commitment from storage backend
				storageRoot, err = storage.Update(evitcedBatch)
				require.NoError(t, err)
				updateTime := time.Since(now)

				// Request storage backend to persist the state
				err = storage.Commit(storageRoot)
				require.NoError(t, err)

				sum.storagePersistTime += time.Since(now) - updateTime
				sum.storageUpdateTime += updateTime
				sum.storagePersistCount++
				sum.storageUpdateCount++

				if writeSnapshot {
					for _, kv := range evitcedBatch {
						if len(kv.Key) == 32 {
							if len(kv.Value) == 0 {
								rawdb.DeleteAccountSnapshot(dbs.chain, common.BytesToHash(kv.Key))
								it := dbs.chain.NewIterator(append(rawdb.SnapshotStoragePrefix, kv.Key...), nil)
								keysDeleted := 0
								for it.Next() {
									k := it.Key()[len(rawdb.SnapshotStoragePrefix):]
									rawdb.DeleteStorageSnapshot(dbs.chain, common.BytesToHash(k[:32]), common.BytesToHash(k[32:]))
									keysDeleted++
								}
								if err := it.Error(); err != nil {
									t.Fatalf("Failed to iterate over snapshot account: %v", err)
								}
								it.Release()
								if keysDeleted > 0 {
									t.Logf("Deleted %d storage keys for account %x", keysDeleted, kv.Key)
								}
							} else {
								var acc types.StateAccount
								if err := rlp.DecodeBytes(kv.Value, &acc); err != nil {
									t.Fatalf("Failed to decode account: %v", err)
								}
								data := types.SlimAccountRLP(acc)
								rawdb.WriteAccountSnapshot(dbs.chain, common.BytesToHash(kv.Key), data)
							}
						} else {
							if len(kv.Value) > 0 {
								rawdb.WriteStorageSnapshot(dbs.chain, common.BytesToHash(kv.Key[:32]), common.BytesToHash(kv.Key[32:]), kv.Value)
							} else {
								rawdb.DeleteStorageSnapshot(dbs.chain, common.BytesToHash(kv.Key[:32]), common.BytesToHash(kv.Key[32:]))
							}
						}
					}

					evitcedBatch = evitcedBatch[:0]
				}
				if sourceDb != nil {
					// update block and metadata from source db
					hash := rawdb.ReadCanonicalHash(sourceDb, blockNumber)
					require.Equal(t, blockHash, hash, "Block hash mismatch")

					block := rawdb.ReadBlock(sourceDb, hash, blockNumber)
					require.NotNil(t, block, "Block not found in source db")

					b := dbs.chain.NewBatch()
					rawdb.WriteCanonicalHash(b, hash, blockNumber)
					rawdb.WriteBlock(b, block)

					// update metadata
					rawdb.WriteAcceptorTip(b, blockHash)
					rawdb.WriteHeadBlockHash(b, blockHash)
					rawdb.WriteHeadHeaderHash(b, blockHash)
					rawdb.WriteSnapshotBlockHash(b, blockHash)
					rawdb.WriteSnapshotRoot(b, block.Root()) // TODO: unsure if this should be block.Root() or storageRoot

					// handle genesis
					if lastCommit.number == 0 {
						genesis := getMainnetGenesis(t)
						genesisHash := genesis.ToBlock().Hash()
						rawdb.WriteCanonicalHash(b, genesisHash, 0)
						rawdb.WriteBlock(b, genesis.ToBlock())
						snapshot.ResetSnapshotGeneration(b)
						t.Logf("Updating genesis hash: %s", genesisHash.TerminalString())
					}

					require.NoError(t, b.Write())

					if storageBackend == "legacy" {
						require.Equal(t, storageRoot, block.Root(), "Root mismatch")
					}

				}

				updateMetadata(t, dbs.metadata, blockHash, storageRoot, blockNumber)
				lastCommit.number = blockNumber
				lastCommit.txs = sum.txs + sum.atomicTxs
			}
			commitLock.Unlock()
		}

		sum.accountReads += uint64(len(tapeResult.accountReads))
		sum.storageReads += uint64(len(tapeResult.storageReads))
		sum.accountWrites += uint64(accountWrites)
		sum.storageWrites += uint64(storageWrites)
		sum.accountUpdates += uint64(accountUpdates)
		sum.storageUpdates += uint64(storageUpdates)
		sum.accountDeletes += uint64(accountDeletes)
		sum.storageDeletes += uint64(storageDeletes)
		sum.accounts += int64(int(accountWrites) - accountUpdates - 2*accountDeletes)
		sum.storage += int64(int(storageWrites) - storageUpdates - 2*storageDeletes)

		if blockNumber%uint64(logEach) == 0 {
			storageRootStr := ""
			if storageRoot != (common.Hash{}) {
				storageRootStr = "/" + storageRoot.TerminalString()
			}
			t.Logf("Block[%s%s]: (%d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d)",
				blockHash.TerminalString(), storageRootStr, blockNumber,
				sum.txs-lastReported.txs, sum.atomicTxs-lastReported.atomicTxs,
				sum.accountReads-lastReported.accountReads, sum.storageReads-lastReported.storageReads,
				sum.accountReadHits-lastReported.accountReadHits, sum.storageReadHits-lastReported.storageReadHits,
				sum.accountWrites-lastReported.accountWrites, sum.storageWrites-lastReported.storageWrites,
				sum.accountWriteHits-lastReported.accountWriteHits, sum.storageWriteHits-lastReported.storageWriteHits,
				sum.accountUpdates-lastReported.accountUpdates, sum.storageUpdates-lastReported.storageUpdates,
				sum.accountDeletes-lastReported.accountDeletes, sum.storageDeletes-lastReported.storageDeletes,
				sum.accounts-lastReported.accounts, sum.storage-lastReported.storage,
			)
			if readCacheBackend != "none" {
				hits := sum.accountReadHits - lastReported.accountReadHits + sum.storageReadHits - lastReported.storageReadHits
				total := sum.accountReads - lastReported.accountReads + sum.storageReads - lastReported.storageReads
				t.Logf(
					"Cache stats: %d hits, %d misses, %.2f hit rate, %d entries (= %.4f of state), %d MiB",
					hits, total-hits, float64(hits)/float64(total),
					cache.Len(), float64(cache.Len())/float64(sum.accounts+sum.storage),
					cache.EstimatedSize()/(units.MiB),
				)
			}
			writeHits := sum.accountWriteHits + sum.storageWriteHits - lastReported.accountWriteHits - lastReported.storageWriteHits
			writeTotal := sum.accountWrites + sum.storageWrites - lastReported.accountWrites - lastReported.storageWrites
			_, oldest, found := writeCache.GetOldest()
			if !found {
				oldest.updatedAt = blockNumber // so displays as 0
			}
			txs := sum.txs + sum.atomicTxs - lastReported.txs - lastReported.atomicTxs
			storageUpdateCount := sum.storageUpdateCount - lastReported.storageUpdateCount
			storageUpdateTime := sum.storageUpdateTime - lastReported.storageUpdateTime
			storageUpdateAvg := int64(0)
			if storageUpdateCount > 0 {
				storageUpdateAvg = storageUpdateTime.Milliseconds() / int64(storageUpdateCount)
			}
			storagePersistCount := sum.storagePersistCount - lastReported.storagePersistCount
			storagePersistTime := sum.storagePersistTime - lastReported.storagePersistTime
			storagePersistAvg := int64(0)
			if storagePersistCount > 0 {
				storagePersistAvg = storagePersistTime.Milliseconds() / int64(storagePersistCount)
			}
			t.Logf(
				"Write cache stats: %d hits, %d misses, %.2f hit rate, %d entries (= %.4f of state), evicted/tx: %.1f acc, %.1f storage (total: %dk) (time: %d, total: %d ms) (updates: %d, time: %d, avg: %d, total: %d ms) (commits: %d, time: %d, avg: %d, total %d ms) (oldest age: %d)",
				writeHits, writeTotal-writeHits, float64(writeHits)/float64(writeTotal),
				writeCache.Len(), float64(writeCache.Len())/float64(sum.accounts+sum.storage),
				float64(sum.writeCacheEvictAccount-lastReported.writeCacheEvictAccount)/float64(txs),
				float64(sum.writeCacheEvictStorage-lastReported.writeCacheEvictStorage)/float64(txs),
				(sum.writeCacheEvictAccount+sum.writeCacheEvictStorage)/1000,
				(sum.writeCacheEvictTime - lastReported.writeCacheEvictTime).Milliseconds(), sum.writeCacheEvictTime.Milliseconds(),
				storageUpdateCount, storageUpdateTime.Milliseconds(), storageUpdateAvg, sum.storageUpdateTime.Milliseconds(),
				storagePersistCount, storagePersistTime.Milliseconds(), storagePersistAvg, sum.storagePersistTime.Milliseconds(),
				blockNumber-oldest.updatedAt,
			)
			quants := []float64{0.05, 0.1, 0.25, 0.5, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95}
			var outString string
			for _, q := range quants {
				val := hst.Quantile(q)
				if val == inf {
					outString = fmt.Sprintf("%s [%.2f inf]", outString, q)
					continue
				}
				outString = fmt.Sprintf("%s [%.2f %d]", outString, q, int(val))
			}
			t.Logf("Write cache quantiles: %s", outString)
			outString = ""
			for _, q := range quants {
				val := hstWithReset.Quantile(q)
				if val == inf {
					outString = fmt.Sprintf("%s [%.2f inf]", outString, q)
					continue
				}
				outString = fmt.Sprintf("%s [%.2f %d]", outString, q, int(val))
			}
			t.Logf("Reset cache quantiles: %s", outString)
			hstWithReset.Reset()
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

func TestXxx(t *testing.T) {
	h := sha3.NewLegacyKeccak256()
	h.Write(common.Hex2Bytes("ac7bbff258e5ff67efcbac43331033010676e22cbf7a9515a31e4e2b661b9b96"))
	fmt.Printf("%x\n", h.Sum(nil))
}
