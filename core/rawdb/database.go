// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ava-labs/libevm/common"
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/leveldb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/ethdb/pebble"
	"github.com/ava-labs/libevm/log"
)

// freezerdb is a database wrapper that enables freezer data retrievals.
type freezerdb struct {
	ancientRoot string
	ethdb.KeyValueStore
	ethdb.AncientStore
}

// AncientDatadir returns the path of root ancient directory.
func (frdb *freezerdb) AncientDatadir() (string, error) {
	return frdb.ancientRoot, nil
}

// Close implements io.Closer, closing both the fast key-value store as well as
// the slow ancient tables.
func (frdb *freezerdb) Close() error {
	var errs []error
	if err := frdb.AncientStore.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := frdb.KeyValueStore.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) != 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// Freeze is a helper method used for external testing to trigger and block until
// a freeze cycle completes, without having to sleep for a minute to trigger the
// automatic background run.
func (frdb *freezerdb) Freeze(threshold uint64) error {
	if frdb.AncientStore.(*chainFreezer).readonly {
		return errReadOnly
	}
	// Set the freezer threshold to a temporary value
	defer func(old uint64) {
		frdb.AncientStore.(*chainFreezer).threshold.Store(old)
	}(frdb.AncientStore.(*chainFreezer).threshold.Load())
	frdb.AncientStore.(*chainFreezer).threshold.Store(threshold)

	// Trigger a freeze cycle and block until it's done
	trigger := make(chan struct{}, 1)
	frdb.AncientStore.(*chainFreezer).trigger <- trigger
	<-trigger
	return nil
}

// nofreezedb is a database wrapper that disables freezer data retrievals.
type nofreezedb struct {
	ethdb.KeyValueStore
}

// HasAncient returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) HasAncient(kind string, number uint64) (bool, error) {
	return false, errNotSupported
}

// Ancient returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Ancient(kind string, number uint64) ([]byte, error) {
	return nil, errNotSupported
}

// AncientRange returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientRange(kind string, start, max, maxByteSize uint64) ([][]byte, error) {
	return nil, errNotSupported
}

// Ancients returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Ancients() (uint64, error) {
	return 0, errNotSupported
}

// Tail returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Tail() (uint64, error) {
	return 0, errNotSupported
}

// AncientSize returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientSize(kind string) (uint64, error) {
	return 0, errNotSupported
}

// ModifyAncients is not supported.
func (db *nofreezedb) ModifyAncients(func(ethdb.AncientWriteOp) error) (int64, error) {
	return 0, errNotSupported
}

// TruncateHead returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) TruncateHead(items uint64) (uint64, error) {
	return 0, errNotSupported
}

// TruncateTail returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) TruncateTail(items uint64) (uint64, error) {
	return 0, errNotSupported
}

// Sync returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Sync() error {
	return errNotSupported
}

func (db *nofreezedb) ReadAncients(fn func(reader ethdb.AncientReaderOp) error) (err error) {
	// Unlike other ancient-related methods, this method does not return
	// errNotSupported when invoked.
	// The reason for this is that the caller might want to do several things:
	// 1. Check if something is in the freezer,
	// 2. If not, check leveldb.
	//
	// This will work, since the ancient-checks inside 'fn' will return errors,
	// and the leveldb work will continue.
	//
	// If we instead were to return errNotSupported here, then the caller would
	// have to explicitly check for that, having an extra clause to do the
	// non-ancient operations.
	return fn(db)
}

// MigrateTable processes the entries in a given table in sequence
// converting them to a new format if they're of an old format.
func (db *nofreezedb) MigrateTable(kind string, convert convertLegacyFn) error {
	return errNotSupported
}

// AncientDatadir returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientDatadir() (string, error) {
	return "", errNotSupported
}

// NewDatabase creates a high level database on top of a given key-value data
// store without a freezer moving immutable chain segments into cold storage.
func NewDatabase(db ethdb.KeyValueStore) ethdb.Database {
	return &nofreezedb{KeyValueStore: db}
}

// resolveChainFreezerDir is a helper function which resolves the absolute path
// of chain freezer by considering backward compatibility.
func resolveChainFreezerDir(ancient string) string {
	// Check if the chain freezer is already present in the specified
	// sub folder, if not then two possibilities:
	// - chain freezer is not initialized
	// - chain freezer exists in legacy location (root ancient folder)
	freezer := path.Join(ancient, ChainFreezerName)
	if !common.FileExist(freezer) {
		if !common.FileExist(ancient) {
			// The entire ancient store is not initialized, still use the sub
			// folder for initialization.
		} else {
			// Ancient root is already initialized, then we hold the assumption
			// that chain freezer is also initialized and located in root folder.
			// In this case fallback to legacy location.
			freezer = ancient
			log.Info("Found legacy ancient chain path", "location", ancient)
		}
	}
	return freezer
}

// NewDatabaseWithFreezer creates a high level database on top of a given key-
// value data store with a freezer moving immutable chain segments into cold
// storage. The passed ancient indicates the path of root ancient directory
// where the chain freezer can be opened.
func NewDatabaseWithFreezer(db ethdb.KeyValueStore, ancient string, namespace string, readonly bool) (ethdb.Database, error) {
	// Create the idle freezer instance
	frdb, err := newChainFreezer(resolveChainFreezerDir(ancient), namespace, readonly)
	if err != nil {
		printChainMetadata(db)
		return nil, err
	}
	// Since the freezer can be stored separately from the user's key-value database,
	// there's a fairly high probability that the user requests invalid combinations
	// of the freezer and database. Ensure that we don't shoot ourselves in the foot
	// by serving up conflicting data, leading to both datastores getting corrupted.
	//
	//   - If both the freezer and key-value store are empty (no genesis), we just
	//     initialized a new empty freezer, so everything's fine.
	//   - If the key-value store is empty, but the freezer is not, we need to make
	//     sure the user's genesis matches the freezer. That will be checked in the
	//     blockchain, since we don't have the genesis block here (nor should we at
	//     this point care, the key-value/freezer combo is valid).
	//   - If neither the key-value store nor the freezer is empty, cross validate
	//     the genesis hashes to make sure they are compatible. If they are, also
	//     ensure that there's no gap between the freezer and subsequently leveldb.
	//   - If the key-value store is not empty, but the freezer is, we might just be
	//     upgrading to the freezer release, or we might have had a small chain and
	//     not frozen anything yet. Ensure that no blocks are missing yet from the
	//     key-value store, since that would mean we already had an old freezer.

	// If the genesis hash is empty, we have a new key-value store, so nothing to
	// validate in this method. If, however, the genesis hash is not nil, compare
	// it to the freezer content.
	if kvgenesis, _ := db.Get(headerHashKey(0)); len(kvgenesis) > 0 {
		if frozen, _ := frdb.Ancients(); frozen > 0 {
			// If the freezer already contains something, ensure that the genesis blocks
			// match, otherwise we might mix up freezers across chains and destroy both
			// the freezer and the key-value store.
			frgenesis, err := frdb.Ancient(ChainFreezerHashTable, 0)
			if err != nil {
				printChainMetadata(db)
				return nil, fmt.Errorf("failed to retrieve genesis from ancient %v", err)
			} else if !bytes.Equal(kvgenesis, frgenesis) {
				printChainMetadata(db)
				return nil, fmt.Errorf("genesis mismatch: %#x (leveldb) != %#x (ancients)", kvgenesis, frgenesis)
			}
			// Key-value store and freezer belong to the same network. Ensure that they
			// are contiguous, otherwise we might end up with a non-functional freezer.
			if kvhash, _ := db.Get(headerHashKey(frozen)); len(kvhash) == 0 {
				// Subsequent header after the freezer limit is missing from the database.
				// Reject startup if the database has a more recent head.
				if head := *ReadHeaderNumber(db, ReadHeadHeaderHash(db)); head > frozen-1 {
					// Find the smallest block stored in the key-value store
					// in range of [frozen, head]
					var number uint64
					for number = frozen; number <= head; number++ {
						if present, _ := db.Has(headerHashKey(number)); present {
							break
						}
					}
					// We are about to exit on error. Print database metadata before exiting
					printChainMetadata(db)
					return nil, fmt.Errorf("gap in the chain between ancients [0 - #%d] and leveldb [#%d - #%d] ",
						frozen-1, number, head)
				}
				// Database contains only older data than the freezer, this happens if the
				// state was wiped and reinited from an existing freezer.
			}
			// Otherwise, key-value store continues where the freezer left off, all is fine.
			// We might have duplicate blocks (crash after freezer write but before key-value
			// store deletion, but that's fine).
		} else {
			// If the freezer is empty, ensure nothing was moved yet from the key-value
			// store, otherwise we'll end up missing data. We check block #1 to decide
			// if we froze anything previously or not, but do take care of databases with
			// only the genesis block.
			if ReadHeadHeaderHash(db) != common.BytesToHash(kvgenesis) {
				// Key-value store contains more data than the genesis block, make sure we
				// didn't freeze anything yet.
				if kvblob, _ := db.Get(headerHashKey(1)); len(kvblob) == 0 {
					printChainMetadata(db)
					return nil, errors.New("ancient chain segments already extracted, please set --datadir.ancient to the correct path")
				}
				// Block #1 is still in the database, we're allowed to init a new freezer
			}
			// Otherwise, the head header is still the genesis, we're allowed to init a new
			// freezer.
		}
	}
	// Freezer is consistent with the key-value database, permit combining the two
	if !frdb.readonly {
		frdb.wg.Add(1)
		go func() {
			frdb.freeze(db)
			frdb.wg.Done()
		}()
	}
	return &freezerdb{
		ancientRoot:   ancient,
		KeyValueStore: db,
		AncientStore:  frdb,
	}, nil
}

// NewMemoryDatabase creates an ephemeral in-memory key-value database without a
// freezer moving immutable chain segments into cold storage.
func NewMemoryDatabase() ethdb.Database {
	return NewDatabase(memorydb.New())
}

// NewMemoryDatabaseWithCap creates an ephemeral in-memory key-value database
// with an initial starting capacity, but without a freezer moving immutable
// chain segments into cold storage.
func NewMemoryDatabaseWithCap(size int) ethdb.Database {
	return NewDatabase(memorydb.NewWithCap(size))
}

// NewLevelDBDatabase creates a persistent key-value database without a freezer
// moving immutable chain segments into cold storage.
func NewLevelDBDatabase(file string, cache int, handles int, namespace string, readonly bool) (ethdb.Database, error) {
	db, err := leveldb.New(file, cache, handles, namespace, readonly)
	if err != nil {
		return nil, err
	}
	log.Info("Using LevelDB as the backing database")
	return NewDatabase(db), nil
}

// NewPebbleDBDatabase creates a persistent key-value database without a freezer
// moving immutable chain segments into cold storage.
func NewPebbleDBDatabase(file string, cache int, handles int, namespace string, readonly, ephemeral bool) (ethdb.Database, error) {
	db, err := pebble.New(file, cache, handles, namespace, readonly, ephemeral)
	if err != nil {
		return nil, err
	}
	return NewDatabase(db), nil
}

const (
	dbPebble  = "pebble"
	dbLeveldb = "leveldb"
)

// PreexistingDatabase checks the given data directory whether a database is already
// instantiated at that location, and if so, returns the type of database (or the
// empty string).
func PreexistingDatabase(path string) string {
	if _, err := os.Stat(filepath.Join(path, "CURRENT")); err != nil {
		return "" // No pre-existing db
	}
	if matches, err := filepath.Glob(filepath.Join(path, "OPTIONS*")); len(matches) > 0 || err != nil {
		if err != nil {
			panic(err) // only possible if the pattern is malformed
		}
		return dbPebble
	}
	return dbLeveldb
}

// OpenOptions contains the options to apply when opening a database.
// OBS: If AncientsDirectory is empty, it indicates that no freezer is to be used.
type OpenOptions struct {
	Type              string // "leveldb" | "pebble"
	Directory         string // the datadir
	AncientsDirectory string // the ancients-dir
	Namespace         string // the namespace for database relevant metrics
	Cache             int    // the capacity(in megabytes) of the data caching
	Handles           int    // number of files to be open simultaneously
	ReadOnly          bool
	// Ephemeral means that filesystem sync operations should be avoided: data integrity in the face of
	// a crash is not important. This option should typically be used in tests.
	Ephemeral bool
}

// openKeyValueDatabase opens a disk-based key-value database, e.g. leveldb or pebble.
//
//	                      type == null          type != null
//	                   +----------------------------------------
//	db is non-existent |  pebble default  |  specified type
//	db is existent     |  from db         |  specified type (if compatible)
func openKeyValueDatabase(o OpenOptions) (ethdb.Database, error) {
	// Reject any unsupported database type
	if len(o.Type) != 0 && o.Type != dbLeveldb && o.Type != dbPebble {
		return nil, fmt.Errorf("unknown db.engine %v", o.Type)
	}
	// Retrieve any pre-existing database's type and use that or the requested one
	// as long as there's no conflict between the two types
	existingDb := PreexistingDatabase(o.Directory)
	if len(existingDb) != 0 && len(o.Type) != 0 && o.Type != existingDb {
		return nil, fmt.Errorf("db.engine choice was %v but found pre-existing %v database in specified data directory", o.Type, existingDb)
	}
	if o.Type == dbPebble || existingDb == dbPebble {
		log.Info("Using pebble as the backing database")
		return NewPebbleDBDatabase(o.Directory, o.Cache, o.Handles, o.Namespace, o.ReadOnly, o.Ephemeral)
	}
	if o.Type == dbLeveldb || existingDb == dbLeveldb {
		log.Info("Using leveldb as the backing database")
		return NewLevelDBDatabase(o.Directory, o.Cache, o.Handles, o.Namespace, o.ReadOnly)
	}
	// No pre-existing database, no user-requested one either. Default to Pebble.
	log.Info("Defaulting to pebble as the backing database")
	return NewPebbleDBDatabase(o.Directory, o.Cache, o.Handles, o.Namespace, o.ReadOnly, o.Ephemeral)
}

// Open opens both a disk-based key-value database such as leveldb or pebble, but also
// integrates it with a freezer database -- if the AncientDir option has been
// set on the provided OpenOptions.
// The passed o.AncientDir indicates the path of root ancient directory where
// the chain freezer can be opened.
func Open(o OpenOptions) (ethdb.Database, error) {
	kvdb, err := openKeyValueDatabase(o)
	if err != nil {
		return nil, err
	}
	if len(o.AncientsDirectory) == 0 {
		return kvdb, nil
	}
	frdb, err := NewDatabaseWithFreezer(kvdb, o.AncientsDirectory, o.Namespace, o.ReadOnly)
	if err != nil {
		kvdb.Close()
		return nil, err
	}
	return frdb, nil
}

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	var (
		codeToFetch   ethrawdb.DatabaseStat
		syncPerformed ethrawdb.DatabaseStat
		syncProgress  ethrawdb.DatabaseStat
		syncSegments  ethrawdb.DatabaseStat
	)

	options := []ethrawdb.InspectDatabaseOption{
		ethrawdb.WithDatabaseMetadataKeys(func(key []byte) bool {
			return bytes.Equal(key, snapshotBlockHashKey) ||
				bytes.Equal(key, syncRootKey)
		}),
		ethrawdb.WithDatabaseStatRecorder(func(key []byte, size common.StorageSize) bool {
			switch {
			case bytes.HasPrefix(key, syncSegmentsPrefix) && len(key) == syncSegmentsKeyLength:
				syncSegments.Add(size)
				return true
			case bytes.HasPrefix(key, syncStorageTriesPrefix) && len(key) == syncStorageTriesKeyLength:
				syncProgress.Add(size)
				return true
			case bytes.HasPrefix(key, CodeToFetchPrefix) && len(key) == codeToFetchKeyLength:
				codeToFetch.Add(size)
				return true
			case bytes.HasPrefix(key, syncPerformedPrefix) && len(key) == syncPerformedKeyLength:
				syncPerformed.Add(size)
				return true
			default:
				return false
			}
		}),
		ethrawdb.WithDatabaseStatsTransformer(func(rows [][]string) [][]string {
			newRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				database := row[0]
				category := row[1]
				switch {
				case database == "Key-Value store" && category == "Difficulties",
					database == "Key-Value store" && category == "Beacon sync headers",
					database == "Ancient store (Chain)":
					// Discard rows specific to libevm (geth) but irrelevant to coreth.
					continue
				}
				newRows = append(newRows, row)
			}

			return append(
				newRows,
				[]string{"State sync", "Trie segments", syncSegments.Size(), syncSegments.Count()},
				[]string{"State sync", "Storage tries to fetch", syncProgress.Size(), syncProgress.Count()},
				[]string{"State sync", "Code to fetch", codeToFetch.Size(), codeToFetch.Count()},
				[]string{"State sync", "Block numbers synced to", syncPerformed.Size(), syncPerformed.Count()},
			)
		}),
	}

	return ethrawdb.InspectDatabase(db, keyPrefix, keyStart, options...)
}

// printChainMetadata prints out chain metadata to stderr.
func printChainMetadata(db ethdb.KeyValueStore) {
	fmt.Fprintf(os.Stderr, "Chain metadata\n")
	for _, v := range ReadChainMetadata(db) {
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Join(v, ": "))
	}
	fmt.Fprintf(os.Stderr, "\n\n")
}

// ReadChainMetadata returns a set of key/value pairs that contains information
// about the database chain status. This can be used for diagnostic purposes
// when investigating the state of the node.
func ReadChainMetadata(db ethdb.KeyValueStore) [][]string {
	pp := func(val *uint64) string {
		if val == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%d (%#x)", *val, *val)
	}
	data := [][]string{
		{"databaseVersion", pp(ReadDatabaseVersion(db))},
		{"headBlockHash", fmt.Sprintf("%v", ReadHeadBlockHash(db))},
		{"headFastBlockHash", fmt.Sprintf("%v", ReadHeadFastBlockHash(db))},
		{"headHeaderHash", fmt.Sprintf("%v", ReadHeadHeaderHash(db))},
		{"lastPivotNumber", pp(ReadLastPivotNumber(db))},
		{"len(snapshotSyncStatus)", fmt.Sprintf("%d bytes", len(ReadSnapshotSyncStatus(db)))},
		{"snapshotDisabled", fmt.Sprintf("%v", ReadSnapshotDisabled(db))},
		{"snapshotJournal", fmt.Sprintf("%d bytes", len(ReadSnapshotJournal(db)))},
		{"snapshotRecoveryNumber", pp(ReadSnapshotRecoveryNumber(db))},
		{"snapshotRoot", fmt.Sprintf("%v", ReadSnapshotRoot(db))},
		{"txIndexTail", pp(ReadTxIndexTail(db))},
	}
	if b := ReadSkeletonSyncStatus(db); b != nil {
		data = append(data, []string{"SkeletonSyncStatus", string(b)})
	}
	return data
}
