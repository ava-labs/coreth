// (c) 2025, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"fmt"
	// TODO: move this to the FFI layer
	"unsafe"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	firewood "github.com/ava-labs/firewood/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
)

type DatabaseHash = common.Hash

// DbView is a view of the database at a given revision.
type IDbView interface {
	// Returns the data stored at the given key at this revision
	Get(key []byte) ([]byte, error)
	// Returns the hash of this view
	Hash() DatabaseHash
}

type IProposal interface {
	// All proposals support read operations
	IDbView
	// Propose updates the proposal with the given keys and values
	// and returns a new proposal.
	Propose(keys [][]byte, values [][]byte) (IProposal, error)
	// Commit commits the proposal as a new revision
	Commit() error
	// Drop drops the proposal; this object may no longer be used after this call.
	Drop() error
}

var _ IProposal = &FirewoodProposal{}

type IFirewood interface {
	// Read operations on the current database root,
	// including the current database hash.
	IDbView

	// Returns whether a given historical root is available
	// TODO: Do we need this? Internally this will be implemented as an empty
	// call to Revision.
	HasRoot(root DatabaseHash) bool

	// Revision returns a new proposal that is a copy of the current database
	// at this revision.
	Revision(root DatabaseHash) (IDbView, error)

	// Update takes a root and a set of keys-values and creates a new proposal
	// and returns the new root.
	// If values[i] is nil, the key is deleted.
	Propose(keys [][]byte, values [][]byte) (IProposal, error)

	// Close closes the database and releases all resources.
	Close() error
}

var _ IFirewood = &FirewoodDatabase{}

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

type FirewoodDatabase struct {
	valid            bool
	fw               firewood.Database
	current_proposal *Proposal
}

var _ IFirewood = &FirewoodDatabase{}

func New(diskdb ethdb.Database, config *Config) *FirewoodDatabase {
	return &FirewoodDatabase{}
}

// Initialized returns an indicator if the state data is already initialized
// according to the state scheme.
func (db *FirewoodDatabase) Scheme() string {
	return customrawdb.FirewoodScheme
}

func (db *FirewoodDatabase) Initialized(root common.Hash) bool {
	return db.HasRoot(root)
}

func (db *FirewoodDatabase) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	// Assert we have the parent root
	if !db.Initialized(parent) {
		return fmt.Errorf("firewooddb: requested parent root %s not found", parent.Hex())
	}

	if db.current_proposal != nil {
		return fmt.Errorf("firewooddb: cannot update while proposal is open")
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

	// TODO: assert that the parent is the current root

	proposal, err := db.Propose(keys, values)
	if err != nil {
		log.Error("Failed to update trie", "parent", parent, "err", err)
	}
	newRoot := proposal.Hash()

	if newRoot != root {
		log.Error("Firewood update returned unexpected root", "expected", root, "actual", newRoot)
		return fmt.Errorf("firewooddb: firewood update returned unexpected root %s, expected %s", newRoot.Hex(), root.Hex())
	}

	db.current_proposal = &proposal.(*FirewoodProposal).proposal
	return nil
}

func (db *FirewoodDatabase) Close() error {
	db.valid = false
	return db.fw.Close()
}

var _ IFirewood = &FirewoodDatabase{}

type FirewoodProposal struct {
	proposal Proposal
}

// Commit implements IProposal.
func (p *FirewoodProposal) Commit() error {
	panic("unimplemented")
}

// Drop implements IProposal.
func (p *FirewoodProposal) Drop() error {
	panic("unimplemented")
}

// Get implements IProposal.
func (p *FirewoodProposal) Get(key []byte) ([]byte, error) {
	panic("unimplemented")
}

// Hash implements IProposal.
func (p *FirewoodProposal) Hash() DatabaseHash {
	panic("unimplemented")
}

// Propose implements IProposal.
func (p *FirewoodProposal) Propose(keys [][]byte, values [][]byte) (IProposal, error) {
	panic("unimplemented")
}

// move this to the FFI layer
type Proposal struct {
	handle unsafe.Pointer
}

var _ IProposal = &FirewoodProposal{}

// Size returns the storage size of diff layer nodes above the persistent disk
// layer and the dirty nodes buffered within the disk layer
func (db *FirewoodDatabase) Size() (common.StorageSize, common.StorageSize) {
	// TODO: Do we even need this? Only used for metrics and Commit intervals (but commits are no-ops)
	return 0, 0
}

// Cap iteratively flushes old but still referenced trie nodes until the total
// memory usage goes below the given threshold. The held pre-images accumulated
// up to this point will be flushed in case the size exceeds the threshold.
func (db *FirewoodDatabase) Cap(limit common.StorageSize) error {
	// Firewood does not support capping
	return nil
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (db *FirewoodDatabase) Reader(root common.Hash) (database.Reader, error) {
	if !db.Initialized(root) {
		return nil, fmt.Errorf("firewooddb: requested state root %s not found", root.Hex())
	}
	return &reader{db: db, root: root}, nil
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	db   *FirewoodDatabase
	root common.Hash
}

// Node retrieves the trie node with the given node hash. No error will be
// returned if the node is not found.
func (reader *reader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	// No reason to return the metaroot
	if hash == (common.Hash{}) {
		return nil, nil
	}

	rev, err := reader.db.Revision(hash)
	if err != nil {
		return nil, err
	}

	key := path
	if owner != (common.Hash{}) {
		key = append(owner.Bytes(), path...) // TODO: Is this right?
	}
	blob, err := rev.Get(key)
	if err != nil {
		return nil, nil
	}
	return blob, nil
}

func (db *FirewoodDatabase) Get(key []byte) ([]byte, error) {
	return db.fw.Get(key)
}

func (db *FirewoodDatabase) Hash() DatabaseHash {
	panic("unimplemented")
}

func (db *FirewoodDatabase) HasRoot(root DatabaseHash) bool {
	panic("unimplemented")
}

func (db *FirewoodDatabase) Revision(root DatabaseHash) (IDbView, error) {
	panic("unimplemented")
}

func (db *FirewoodDatabase) Propose(keys [][]byte, values [][]byte) (IProposal, error) {
	panic("unimplemented")
}

func (db *FirewoodDatabase) Commit(root common.Hash, report bool) error {
	// assert that the current proposal is not nil
	if db.current_proposal == nil {
		return fmt.Errorf("firewooddb: cannot commit while proposal is nil")
	}
	// assert that the current proposal is the same as the root
	if db.current_proposal.Hash() != root {
		return fmt.Errorf("firewooddb: cannot commit while proposal is not the same as the root")
	}

	// TODO: db.current_proposal.Commit()

	db.current_proposal = nil
	return nil	
}
