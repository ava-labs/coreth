// (c) 2025, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"errors"
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

type ProposalContext struct {
	// The actual proposal
	Proposal IProposal
	// The parent of the proposal
	Parent common.Hash
	// The block number of the proposal
	Block uint64
}

// DbView is a view of the database at a given revision.
type IDbView interface {
	// Returns the data stored at the given key at this revision
	Get(key []byte) ([]byte, error)
	// Returns the hash of this view
	Hash() []byte
}

type IProposal interface {
	// All proposals support read operations
	IDbView
	// Propose takes a root and a set of keys-values and creates a new proposal
	// and returns the new root.
	// If values[i] is nil, the key is deleted.
	Propose(keys [][]byte, values [][]byte) (IProposal, error)
	// Commit commits the proposal as a new revision
	Commit() error
	// Drop drops the proposal; this object may no longer be used after this call.
	Drop() error
}

type IFirewood interface {
	// Read operations on the current database root,
	// including the current database hash.
	IDbView
	// Update takes a root and a set of keys-values and creates a new proposal
	// and returns the new root.
	// If values[i] is nil, the key is deleted.
	Propose(keys [][]byte, values [][]byte) (IProposal, error)
	// Returns the cuurrent root of the database.
	Root() []byte
	// Revision returns a new proposal that is a copy of the current database
	// at this revision.
	Revision(root []byte) (IDbView, error)
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
	fwDisk    IFirewood
	proposals map[common.Hash][]*ProposalContext
}

func New(diskdb ethdb.Database, config *Config) *Database {
	return &Database{
		fwDisk:    nil, // TODO: Initialize with the actual Firewood database.
		proposals: make(map[common.Hash][]*ProposalContext),
	}
}

// Scheme returns the scheme of the database.
func (db *Database) Scheme() string {
	return customrawdb.FirewoodScheme
}

// Initialized indicates whether the most recent root of the database
// matches the given root.
func (db *Database) Initialized(root common.Hash) bool {
	return common.BytesToHash(db.fwDisk.Root()) == root
}

// Update takes a root and a set of keys-values and creates a new proposal.
// It will not be committed until the Commit method is called.
func (db *Database) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	// Create key-value pairs for the nodes in bytes.
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
	// Empty blocks are not allowed in coreth anyway.
	if len(keys) == 0 {
		return errors.New("firewooddb: no keys to update")
	}
	// Create a new proposal with the given keys and values.
	p, err := db.propose(parent, block, keys, values)
	// If we were unable to create a new proposal, return an error.
	if err != nil {
		log.Error("Failed to update trie", "parent", parent, "err", err)
	}

	// Assert that the proposal has the correct root.
	newRoot := common.BytesToHash(p.Hash())
	if newRoot != root {
		log.Error("Firewood update returned unexpected root", "expected", root, "actual", newRoot)
		return fmt.Errorf("firewooddb: firewood update returned unexpected root %s, expected %s", newRoot.Hex(), root.Hex())
	}

	// Store the proposal context.
	pContext := &ProposalContext{
		Proposal: p,
		Parent:   parent,
		Block:    block,
	}
	db.proposals[root] = append(db.proposals[root], pContext)

	return nil
}

// propose creates a new proposal with the given keys and values.
// If the parent cannot be found, an error will be returned.
func (db *Database) propose(parent common.Hash, block uint64, keys [][]byte, values [][]byte) (p IProposal, err error) {
	// If parent is root, we should propose from the db.
	if db.Initialized(parent) {
		p, err = db.fwDisk.Propose(keys, values)
	} else {
		// If the parent is not the root of the database,
		// we need to find the parent proposal.
		possibleProposals, ok := db.proposals[parent]
		if !ok {
			return nil, fmt.Errorf("firewooddb: parent proposal not found for %s", parent.Hex())
		}

		// Find the proposal with the correct parent height.
		var parentProposal *ProposalContext
		for _, proposal := range possibleProposals {
			if proposal.Block == block-1 {
				parentProposal = proposal
				break
			}
		}
		if parentProposal == nil {
			return nil, fmt.Errorf("firewooddb: parent proposal not found for %s of correct height", parent.Hex())
		}

		// Create a new proposal from the parent proposal.
		p, err = parentProposal.Proposal.Propose(keys, values)
	}
	return p, err
}

func (db *Database) Close() error {
	return db.fwDisk.Close()
}

// Commit persists a proposal as a revision to the database.
func (db *Database) Commit(root common.Hash, report bool) error {
	// Find the proposal with the given root.
	// I.e. the proposal in which the parent root is the root of the database.
	var p IProposal
	diskRoot := common.BytesToHash(db.fwDisk.Root())
	for _, pCtx := range db.proposals[root] {
		if pCtx.Parent == diskRoot {
			p = pCtx.Proposal
			break
		}
	}
	if p == nil {
		return fmt.Errorf("firewooddb: proposal not found for %s", root.Hex())
	}

	// Commit the proposal to the database.
	if err := p.Commit(); err != nil {
		log.Error("Failed to commit trie", "node", root, "err", err)
	}

	logger := log.Info
	if !report {
		logger = log.Debug
	}
	logger("Persisted trie from memory database", "node", root)

	// Committing removed the proposal in the backend.
	// We can now drop our reference to it.
	for i, pCtx := range db.proposals[root] {
		if pCtx.Proposal == p {
			db.proposals[root] = append(db.proposals[root][:i], db.proposals[root][i+1:]...)
			break
		}
	}
	return nil
}

// Size returns the storage size of diff layer nodes above the persistent disk
// layer and the dirty nodes buffered within the disk layer
func (db *Database) Size() (common.StorageSize, common.StorageSize) {
	// TODO: Do we even need this? Only used for metrics and Commit intervals in APIs.
	return 0, 0
}

// This isn't called anywhere in coreth
func (db *Database) Reference(_ common.Hash) error {
	return fmt.Errorf("firewooddb: Reference not implemented")
}

// Dereference drops a proposal from the database.
func (db *Database) Dereference(root common.Hash) error {
	// Find the proposal of given root with maximal height.
	// TODO: Ensure `Reject` calls will not generate orphans in the tree.
	var p IProposal
	currentHeight := uint64(0)
	for _, pCtx := range db.proposals[root] {
		if pCtx.Block > currentHeight {
			currentHeight = pCtx.Block
			p = pCtx.Proposal
		}
	}

	// If the proposal is not found in our map, return an error.
	// The map has been corrupted.
	if p == nil {
		return fmt.Errorf("firewooddb: proposal not found for %s", root.Hex())
	}
	return p.Drop()
}

// Cap iteratively flushes old but still referenced trie nodes until the total
// memory usage goes below the given threshold. The held pre-images accumulated
// up to this point will be flushed in case the size exceeds the threshold.
func (db *Database) Cap(limit common.StorageSize) error {
	// Firewood does not support capping
	return nil
}

// viewAtRoot returns a view of the database at the given root.
// An error will be returned if the requested state is not available.
func (db *Database) viewAtRoot(root common.Hash) (IDbView, error) {
	view, err := db.fwDisk.Revision(root.Bytes())
	if err != nil {
		return nil, fmt.Errorf("firewooddb: error retrieving revision forstate root %s", root.Hex())
	}

	if view != nil {
		// Check revisions
		return view, nil
	}

	// Check if the state root corresponds with a proposal.
	// Any proposal with the same root is permissible.

	return nil, fmt.Errorf("firewooddb: requested state root %s not found", root.Hex())
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (db *Database) Reader(root common.Hash) (database.Reader, error) {
	// Check if we can currently read the requested root
	_, err := db.viewAtRoot(root)
	if err != nil {
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

	// Ensure we have access to the requested root
	view, err := reader.db.viewAtRoot(reader.root)
	if err != nil {
		return nil, fmt.Errorf("firewooddb: requested state root %s not found", reader.root.Hex()) // TODO: Should we return the error?
	}

	key := path
	if owner != (common.Hash{}) {
		key = append(owner.Bytes(), path...) // TODO: Is this right?
	}
	blob, err := view.Get(key)
	if err != nil {
		return nil, nil
	}
	return blob, nil
}
