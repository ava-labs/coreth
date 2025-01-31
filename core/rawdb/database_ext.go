// (c) 2019-2025, Ava Labs, Inc.
package rawdb

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/olekukonko/tablewriter"
)

// ClearPrefix removes all keys in db that begin with prefix and match an
// expected key length. [keyLen] should include the length of the prefix.
func ClearPrefix(db ethdb.KeyValueStore, prefix []byte, keyLen int) error {
	it := db.NewIterator(prefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		key := common.CopyBytes(it.Key())
		if len(key) != keyLen {
			// avoid deleting keys that do not match the expected length
			continue
		}
		if err := batch.Delete(key); err != nil {
			return err
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	it := db.NewIterator(keyPrefix, keyStart)
	defer it.Release()

	var (
		count  int64
		start  = time.Now()
		logged = time.Now()

		// Key-value store statistics
		headers         stat
		bodies          stat
		receipts        stat
		tds             stat
		numHashPairings stat
		hashNumPairings stat
		legacyTries     stat
		stateLookups    stat
		accountTries    stat
		storageTries    stat
		codes           stat
		txLookups       stat
		accountSnaps    stat
		storageSnaps    stat
		preimages       stat
		bloomBits       stat
		beaconHeaders   stat
		cliqueSnaps     stat

		// State sync statistics
		codeToFetch   stat
		syncProgress  stat
		syncSegments  stat
		syncPerformed stat

		// Les statistic
		chtTrieNodes   stat
		bloomTrieNodes stat

		// Meta- and unaccounted data
		metadata    stat
		unaccounted stat

		// Totals
		total common.StorageSize
	)
	// Inspect key-value database first.
	for it.Next() {
		var (
			key  = it.Key()
			size = common.StorageSize(len(key) + len(it.Value()))
		)
		total += size
		switch {
		case bytes.HasPrefix(key, headerPrefix) && len(key) == (len(headerPrefix)+8+common.HashLength):
			headers.Add(size)
		case bytes.HasPrefix(key, blockBodyPrefix) && len(key) == (len(blockBodyPrefix)+8+common.HashLength):
			bodies.Add(size)
		case bytes.HasPrefix(key, blockReceiptsPrefix) && len(key) == (len(blockReceiptsPrefix)+8+common.HashLength):
			receipts.Add(size)
		case bytes.HasPrefix(key, headerPrefix) && bytes.HasSuffix(key, headerTDSuffix):
			tds.Add(size)
		case bytes.HasPrefix(key, headerPrefix) && bytes.HasSuffix(key, headerHashSuffix):
			numHashPairings.Add(size)
		case bytes.HasPrefix(key, headerNumberPrefix) && len(key) == (len(headerNumberPrefix)+common.HashLength):
			hashNumPairings.Add(size)
		case IsLegacyTrieNode(key, it.Value()):
			legacyTries.Add(size)
		case bytes.HasPrefix(key, stateIDPrefix) && len(key) == len(stateIDPrefix)+common.HashLength:
			stateLookups.Add(size)
		case IsAccountTrieNode(key):
			accountTries.Add(size)
		case IsStorageTrieNode(key):
			storageTries.Add(size)
		case bytes.HasPrefix(key, CodePrefix) && len(key) == len(CodePrefix)+common.HashLength:
			codes.Add(size)
		case bytes.HasPrefix(key, txLookupPrefix) && len(key) == (len(txLookupPrefix)+common.HashLength):
			txLookups.Add(size)
		case bytes.HasPrefix(key, SnapshotAccountPrefix) && len(key) == (len(SnapshotAccountPrefix)+common.HashLength):
			accountSnaps.Add(size)
		case bytes.HasPrefix(key, SnapshotStoragePrefix) && len(key) == (len(SnapshotStoragePrefix)+2*common.HashLength):
			storageSnaps.Add(size)
		case bytes.HasPrefix(key, PreimagePrefix) && len(key) == (len(PreimagePrefix)+common.HashLength):
			preimages.Add(size)
		case bytes.HasPrefix(key, configPrefix) && len(key) == (len(configPrefix)+common.HashLength):
			metadata.Add(size)
		case bytes.HasPrefix(key, genesisPrefix) && len(key) == (len(genesisPrefix)+common.HashLength):
			metadata.Add(size)
		case bytes.HasPrefix(key, bloomBitsPrefix) && len(key) == (len(bloomBitsPrefix)+10+common.HashLength):
			bloomBits.Add(size)
		case bytes.HasPrefix(key, BloomBitsIndexPrefix):
			bloomBits.Add(size)
		case bytes.HasPrefix(key, skeletonHeaderPrefix) && len(key) == (len(skeletonHeaderPrefix)+8):
			beaconHeaders.Add(size)
		case bytes.HasPrefix(key, CliqueSnapshotPrefix) && len(key) == 7+common.HashLength:
			cliqueSnaps.Add(size)
		case bytes.HasPrefix(key, ChtTablePrefix) ||
			bytes.HasPrefix(key, ChtIndexTablePrefix) ||
			bytes.HasPrefix(key, ChtPrefix): // Canonical hash trie
			chtTrieNodes.Add(size)
		case bytes.HasPrefix(key, BloomTrieTablePrefix) ||
			bytes.HasPrefix(key, BloomTrieIndexPrefix) ||
			bytes.HasPrefix(key, BloomTriePrefix): // Bloomtrie sub
			bloomTrieNodes.Add(size)
		case bytes.HasPrefix(key, syncStorageTriesPrefix) && len(key) == syncStorageTriesKeyLength:
			syncProgress.Add(size)
		case bytes.HasPrefix(key, syncSegmentsPrefix) && len(key) == syncSegmentsKeyLength:
			syncSegments.Add(size)
		case bytes.HasPrefix(key, CodeToFetchPrefix) && len(key) == codeToFetchKeyLength:
			codeToFetch.Add(size)
		case bytes.HasPrefix(key, syncPerformedPrefix) && len(key) == syncPerformedKeyLength:
			syncPerformed.Add(size)
		default:
			var accounted bool
			for _, meta := range [][]byte{
				databaseVersionKey, headHeaderKey, headBlockKey, headFastBlockKey, headFinalizedBlockKey,
				lastPivotKey, fastTrieProgressKey, snapshotDisabledKey, SnapshotRootKey, snapshotJournalKey,
				snapshotGeneratorKey, snapshotRecoveryKey, txIndexTailKey, fastTxLookupLimitKey,
				uncleanShutdownKey, badBlockKey, transitionStatusKey, skeletonSyncStatusKey,
				persistentStateIDKey, trieJournalKey, snapshotSyncStatusKey, snapSyncStatusFlagKey,
				snapshotBlockHashKey, syncRootKey,
			} {
				if bytes.Equal(key, meta) {
					metadata.Add(size)
					accounted = true
					break
				}
			}
			if !accounted {
				unaccounted.Add(size)
			}
		}
		count++
		if count%1000 == 0 && time.Since(logged) > 8*time.Second {
			log.Info("Inspecting database", "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	// Display the database statistic of key-value store.
	stats := [][]string{
		{"Key-Value store", "Headers", headers.Size(), headers.Count()},
		{"Key-Value store", "Bodies", bodies.Size(), bodies.Count()},
		{"Key-Value store", "Receipt lists", receipts.Size(), receipts.Count()},
		{"Key-Value store", "Block number->hash", numHashPairings.Size(), numHashPairings.Count()},
		{"Key-Value store", "Block hash->number", hashNumPairings.Size(), hashNumPairings.Count()},
		{"Key-Value store", "Transaction index", txLookups.Size(), txLookups.Count()},
		{"Key-Value store", "Bloombit index", bloomBits.Size(), bloomBits.Count()},
		{"Key-Value store", "Contract codes", codes.Size(), codes.Count()},
		{"Key-Value store", "Hash trie nodes", legacyTries.Size(), legacyTries.Count()},
		{"Key-Value store", "Path trie state lookups", stateLookups.Size(), stateLookups.Count()},
		{"Key-Value store", "Path trie account nodes", accountTries.Size(), accountTries.Count()},
		{"Key-Value store", "Path trie storage nodes", storageTries.Size(), storageTries.Count()},
		{"Key-Value store", "Trie preimages", preimages.Size(), preimages.Count()},
		{"Key-Value store", "Account snapshot", accountSnaps.Size(), accountSnaps.Count()},
		{"Key-Value store", "Storage snapshot", storageSnaps.Size(), storageSnaps.Count()},
		{"Key-Value store", "Clique snapshots", cliqueSnaps.Size(), cliqueSnaps.Count()},
		{"Key-Value store", "Singleton metadata", metadata.Size(), metadata.Count()},
		{"Light client", "CHT trie nodes", chtTrieNodes.Size(), chtTrieNodes.Count()},
		{"Light client", "Bloom trie nodes", bloomTrieNodes.Size(), bloomTrieNodes.Count()},
		{"State sync", "Trie segments", syncSegments.Size(), syncSegments.Count()},
		{"State sync", "Storage tries to fetch", syncProgress.Size(), syncProgress.Count()},
		{"State sync", "Code to fetch", codeToFetch.Size(), codeToFetch.Count()},
		{"State sync", "Block numbers synced to", syncPerformed.Size(), syncPerformed.Count()},
	}
	// Inspect all registered append-only file store then.
	ancients, err := inspectFreezers(db)
	if err != nil {
		return err
	}
	for _, ancient := range ancients {
		for _, table := range ancient.sizes {
			stats = append(stats, []string{
				fmt.Sprintf("Ancient store (%s)", strings.Title(ancient.name)),
				strings.Title(table.name),
				table.size.String(),
				fmt.Sprintf("%d", ancient.count()),
			})
		}
		total += ancient.size()
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Database", "Category", "Size", "Items"})
	table.SetFooter([]string{"", "Total", total.String(), " "})
	table.AppendBulk(stats)
	table.Render()

	if unaccounted.size > 0 {
		log.Error("Database contains unaccounted data", "size", unaccounted.size, "count", unaccounted.count)
	}
	return nil
}
