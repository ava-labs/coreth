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
	// The root of the proposal
	Root common.Hash
	// The block number of the proposal
	Block uint64
	// The parent of the proposal
	Parent *ProposalContext
	// The children of the proposal
	Children []*ProposalContext
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
	fwDisk IFirewood

	// TODO: These need mutex protection
	proposalMap  map[common.Hash][]*ProposalContext
	proposalTree *ProposalContext
}

func New(diskdb ethdb.Database, config *Config) *Database {
	return &Database{
		fwDisk:      nil, // TODO: Initialize with the actual Firewood database.
		proposalMap: make(map[common.Hash][]*ProposalContext),
		proposalTree: &ProposalContext{
			Proposal: nil,
			Root:     common.Hash{},
			Block:    0,
			Parent:   nil,
			Children: nil,
		},
	}
}

// Scheme returns the scheme of the database.
func (db *Database) Scheme() string {
	return customrawdb.FirewoodScheme
}

// Initialized indicates whether the most recent root of the database
// matches the given root.
func (db *Database) Initialized(root common.Hash) bool {
	// Shouldn't use proposal tree root here, since it may be empty.
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
	return db.propose(root, parent, block, keys, values)
}

// propose creates a new proposal with the given keys and values.
// If the parent cannot be found, an error will be returned.
func (db *Database) propose(root common.Hash, parent common.Hash, block uint64, keys [][]byte, values [][]byte) error {
	// If parent is root, we should propose from the db.
	// Special case: before state syncing, we initialize the database with the empty hash.
	// This is the only time we can propose a different parent root, since all syncing changes
	// are directly written to disk.
	if db.Initialized(parent) && (db.proposalTree.Root == common.Hash{} || db.proposalTree.Block == block-1) {
		p, err := db.fwDisk.Propose(keys, values)
		if err != nil {
			return fmt.Errorf("firewooddb: error proposing from root %s", parent.Hex())
		}

		// Store the proposal context.
		pContext := &ProposalContext{
			Proposal: p,
			Root:     root,
			Block:    block,
			Parent:   db.proposalTree,
		}
		db.proposalMap[root] = append(db.proposalMap[root], pContext)
		db.proposalTree.Children = append(db.proposalTree.Children, pContext)
		return nil
	}

	// If the parent is not the root of the database,
	// we need to find all possible parent proposals.
	possibleProposals, ok := db.proposalMap[parent]
	if !ok {
		return fmt.Errorf("firewooddb: parent proposal not found for %s", parent.Hex())
	}

	// Find all proposals with the correct parent height.
	// We must create a new proposal for each one.
	for _, parentProposal := range possibleProposals {
		if parentProposal.Block == block-1 {
			p, err := parentProposal.Proposal.Propose(keys, values)
			if err != nil {
				return fmt.Errorf("firewooddb: error proposing from parent proposal %s", parent.Hex())
			}
			// Store the proposal context.
			pContext := &ProposalContext{
				Proposal: p,
				Root:     root,
				Block:    block,
				Parent:   parentProposal,
			}
			db.proposalMap[root] = append(db.proposalMap[root], pContext)
			parentProposal.Children = append(parentProposal.Children, pContext)
		}
	}

	return nil
}

func (db *Database) Close() error {
	return db.fwDisk.Close()
}

// Commit persists a proposal as a revision to the database.
func (db *Database) Commit(root common.Hash, report bool) error {
	// Find the proposal with the given root.
	// I.e. the proposal in which the parent root is the root of the database.
	// If this isn't unique, they must be identical, and we can just take the first one.
	var pCtx *ProposalContext
	for _, possible := range db.proposalMap[root] {
		if possible.Parent.Root == db.proposalTree.Root {
			pCtx = possible
			break
		}
	}
	if pCtx == nil {
		return fmt.Errorf("firewooddb: proposal not found for %s", root.Hex())
	}

	// Commit the proposal to the database.
	if err := pCtx.Proposal.Commit(); err != nil {
		return fmt.Errorf("firewooddb: error committing proposal %s", root.Hex())
	}

	logger := log.Info
	if !report {
		logger = log.Debug
	}
	logger("Persisted trie from memory database", "node", root)

	// Committing removed the proposal in the backend.
	// We can now remove our reference and promote the context.
	otherChildren := db.proposalTree.Children
	db.proposalTree = pCtx
	db.proposalTree.Parent = nil // We should not index historical revisions here.
	// Remove the proposal from the map.
	rootList := db.proposalMap[root]
	for i, p := range rootList {
		if p == pCtx { // pointer comparison
			rootList = append(rootList[:i], rootList[i+1:]...) // There could be other proposals with the same root.
			break
		}
	}
	db.proposalMap[root] = rootList

	for _, childCtx := range otherChildren {
		// TODO: Depending on FFI, we may want to dereference the committed proposal as well.
		if childCtx != pCtx {
			db.dereference(childCtx)
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
	// Find the proposal of given root.
	var pCtx *ProposalContext
	count := 0
	for _, possible := range db.proposalMap[root] {
		pCtx = possible
		count++
	}

	// If there are multiple proposals with the same root, we cannot dereference,
	// as we do not know the parent or height.
	if count > 1 {
		log.Debug("Cannot dereference root with multiple proposals", "root", root.Hex(), "count", count)
		return nil // will be cleaned up eventually on later commit
	} else if count == 0 {
		log.Debug("No proposal to dereference found", "root", root.Hex())
		return nil // no error, may have already been dropped
	}

	return db.dereference(pCtx)
}

func (db *Database) dereference(pCtx *ProposalContext) error {
	// Base case: if there are children, we need to dereference them first.
	for _, child := range pCtx.Children {
		if err := db.dereference(child); err != nil {
			return fmt.Errorf("firewooddb: error dereferencing child proposal %s", child.Root.Hex())
		}
	}

	// Drop the proposal in the backend.
	if err := pCtx.Proposal.Drop(); err != nil {
		return fmt.Errorf("firewooddb: error dropping proposal %s", pCtx.Root.Hex())
	}

	// Remove the proposal from the map.
	rootList := db.proposalMap[pCtx.Root]
	for i, p := range rootList {
		if p == pCtx { // pointer comparison
			rootList = append(rootList[:i], rootList[i+1:]...) // There could be other proposals with the same root.
			break
		}
	}
	db.proposalMap[pCtx.Root] = rootList

	// Remove from parent's children.
	if pCtx.Parent != nil {
		for i, child := range pCtx.Parent.Children {
			if child == pCtx { // pointer comparison
				pCtx.Parent.Children = append(pCtx.Parent.Children[:i], pCtx.Parent.Children[i+1:]...)
				break
			}
		}
	}

	return nil
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
		return nil, fmt.Errorf("firewooddb: error retrieving revision for state root %s", root.Hex())
	}

	if view != nil {
		// Found valid revision
		return view, nil
	}

	// Check if the state root corresponds with a proposal.
	proposals, ok := db.proposalMap[root]
	if !ok || len(proposals) == 0 {
		return nil, fmt.Errorf("firewooddb: requested state root %s not found", root.Hex())
	}

	// If there are multiple proposals with the same root, we can use the first one.
	if len(proposals) > 1 {
		log.Debug("Multiple proposals found for root", "root", root.Hex(), "count", len(proposals))
	}
	// Use the first proposal
	return proposals[0].Proposal, nil
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
