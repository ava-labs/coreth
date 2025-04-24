// (c) 2025, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"fmt"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
)

type IFirewood interface {
	// Returns whether a given root is available
	HasRoot(root []byte) bool

	// Returns the data stored at a given node.
	Get(root []byte, key []byte) ([]byte, error)

	// Update takes a root and a set of keys-values and creates a new proposal
	// and returns the new root.
	// If values[i] is nil, the key is deleted.
	Update(parent []byte, keys [][]byte, values [][]byte) ([]byte, error)

	// Commit commits a proposal as a revision to the database.
	Commit(root []byte) error

	// Drop deletes a proposal from the database.
	// This may be called on a revision that has already been dropped.
	// An error should not be returned in this case.
	Drop(root []byte) error

	// Close closes the database and releases all resources.
	Close() error
}

// Config contains the settings for database.
type Config struct {
	Create            bool
	NodeCacheEntries  uint
	Revisions         uint
	ReadCacheStrategy uint8
	MetricsPort       uint16
}

func (c Config) BackendConstructor(diskdb ethdb.Database) triedb.DBOverride {
	return New(diskdb, &c)
}

// Defaults is the default setting for database if it's not specified.
var Defaults = &Config{}

type Database struct {
	fw IFirewood
}

func New(diskdb ethdb.Database, config *Config) *Database {
	return &Database{}
}

// Initialized returns an indicator if the state data is already initialized
// according to the state scheme.
func (db *Database) Scheme() string {
	return customrawdb.FirewoodScheme
}

func (db *Database) Initialized(root common.Hash) bool {
	return db.fw.HasRoot(root.Bytes())
}

func (db *Database) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	// Assert we have the parent root
	if !db.Initialized(parent) {
		return fmt.Errorf("firewooddb: requested parent root %s not found", parent.Hex())
	}

	flattenedNodes := nodes.Flatten()
	var keys [][]byte
	var values [][]byte

	for owner, set := range flattenedNodes {
		for _, n := range set {
			var key []byte
			if owner == (common.Hash{}) {
				key = n.Hash.Bytes()
			} else {
				key = append(owner.Bytes(), n.Hash.Bytes()...)
			}
			keys = append(keys, key)
			values = append(values, n.Blob)
		}
	}

	// Firewood ffi does not accept empty bashes, so if the keys are empty, the root is returned.
	if len(keys) == 0 {
		return nil
	}

	bytes, err := db.fw.Update(parent.Bytes(), keys, values)
	if err != nil {
		log.Error("Failed to update trie", "parent", parent, "err", err)
	}
	newRoot := common.BytesToHash(bytes)

	if newRoot != root {
		log.Error("Firewood update returned unexpected root", "expected", root, "actual", newRoot)
		return fmt.Errorf("firewooddb: firewood update returned unexpected root %s, expected %s", newRoot.Hex(), root.Hex())
	}
	return nil
}

func (db *Database) Close() error {
	return db.fw.Close()
}

// Commit persists a proposal as a revision to the database.
func (db *Database) Commit(root common.Hash, report bool) error {
	logger := log.Info
	if !report {
		logger = log.Debug
	}
	logger("Persisted trie from memory database", "node", root)
	if err := db.fw.Commit(root.Bytes()); err != nil {
		log.Error("Failed to commit trie", "node", root, "err", err)
	}
	return nil
}

// Size returns the storage size of diff layer nodes above the persistent disk
// layer and the dirty nodes buffered within the disk layer
func (db *Database) Size() (common.StorageSize, common.StorageSize) {
	// TODO: Do we even need this? Only used for metrics and Commit intervals (but commits are no-ops)
	return 0, 0
}

// This isn't called anywhere in coreth
func (db *Database) Reference(_ common.Hash) error {
	return fmt.Errorf("firewooddb: Reference not implemented")
}

// Dereference drops a proposal from the database.
func (db *Database) Dereference(root common.Hash) error {
	return db.fw.Drop(root.Bytes())
}

// Cap iteratively flushes old but still referenced trie nodes until the total
// memory usage goes below the given threshold. The held pre-images accumulated
// up to this point will be flushed in case the size exceeds the threshold.
func (db *Database) Cap(limit common.StorageSize) error {
	// Firewood does not support capping
	return nil
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (db *Database) Reader(root common.Hash) (database.Reader, error) {
	if !db.Initialized(root) {
		return nil, fmt.Errorf("firewooddb: requested state root %s not found", root.Hex())
	}
	return &reader{db: db, root: root}, nil
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	db   *Database
	root common.Hash
}

// Node retrieves the trie node with the given node hash. No error will be
// returned if the node is not found.
func (reader *reader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	// No reason to return the metaroot
	if hash == (common.Hash{}) {
		return nil, nil
	}

	key := path
	if owner != (common.Hash{}) {
		key = append(owner.Bytes(), path...) // TODO: Is this right?
	}
	blob, err := reader.db.fw.Get(reader.root.Bytes(), key)
	if err != nil {
		return nil, nil
	}
	return blob, nil
}
