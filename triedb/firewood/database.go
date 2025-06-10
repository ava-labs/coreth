// (c) 2025, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"

	ffi "github.com/ava-labs/firewood-go/ffi"
)

var (
	_ IFirewood = &ffi.Database{}
	_ IProposal = &ffi.Proposal{}
	_ IDbView   = &ffi.Revision{}
)

type ProposalContext struct {
	// The actual proposal
	Proposal *ffi.Proposal
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
}

type IProposal interface {
	// All proposals support read operations
	IDbView
	// Read the current root of the database.
	Root() ([]byte, error)
	// Propose takes a root and a set of keys-values and creates a new proposal
	// and returns the new root.
	// If values[i] is nil, the key is deleted.
	Propose(keys [][]byte, values [][]byte) (*ffi.Proposal, error)
	// Commit commits the proposal as a new revision
	Commit() error
	// Drop drops the proposal; this object may no longer be used after this call.
	Drop() error
}

type IFirewood interface {
	// Read the current root of the database.
	Root() ([]byte, error)
	// Propose takes a root and a set of keys-values and creates a new proposal
	// and returns the new root.
	// If values[i] is nil, the key is deleted.
	Propose(keys [][]byte, values [][]byte) (*ffi.Proposal, error)
	// Revision returns a new view that is a copy of the current database
	// at this historical revision.
	Revision(root []byte) (*ffi.Revision, error)
	// Close closes the database and releases all resources.
	Close() error
}

// Config contains the settings for database.
type TrieDBConfig struct {
	FileName          string
	CleanCacheSize    int // Size of the clean cache in bytes
	Revisions         uint
	ReadCacheStrategy ffi.CacheStrategy
	MetricsPort       uint16
	Database          *Database
}

var Defaults = &TrieDBConfig{
	FileName:          "firewood",
	CleanCacheSize:    1024 * 1024, // 1MB
	Revisions:         100,
	ReadCacheStrategy: ffi.CacheBranchReads,
	MetricsPort:       0, // Default metrics port
}

// Must take reference to allow closure - reuse any existing database for the same config.
func (c *TrieDBConfig) BackendConstructor(diskdb ethdb.Database) triedb.DBOverride {
	if c.Database == nil {
		c.Database = New(diskdb, c)
		return c.Database
	}
	return c.Database
}

type Database struct {
	fwDisk IFirewood
	ethdb  ethdb.Database // The underlying disk database, used for storing genesis and the path.

	proposalLock sync.RWMutex
	proposalMap  map[common.Hash][]*ProposalContext
	proposalTree *ProposalContext
}

func New(diskdb ethdb.Database, trieConfig *TrieDBConfig) *Database {
	if trieConfig == nil {
		log.Error("firewood: no config provided")
		return nil
	}

	config, path, err := validateConfig(diskdb, trieConfig)
	if err != nil {
		log.Error("firewood: error validating config", "error", err)
		return nil
	}

	fw, err := ffi.New(path, config)
	if err != nil {
		log.Error("firewood: error creating firewood database", "error", err)
		return nil
	}

	currentRoot, err := fw.Root()
	if err != nil {
		log.Error("firewood: error getting current root", "error", err)
		return nil
	}

	return &Database{
		fwDisk:      fw,
		ethdb:       diskdb,
		proposalMap: make(map[common.Hash][]*ProposalContext),
		proposalTree: &ProposalContext{
			Proposal: nil,
			Root:     common.Hash(currentRoot),
			Block:    0,
			Parent:   nil,
			Children: nil,
		},
	}
}

func validateConfig(diskdb ethdb.Database, trieConfig *TrieDBConfig) (*ffi.Config, string, error) {
	// Get the path from the database
	path, err := customrawdb.ReadDatabasePath(diskdb)
	if err != nil {
		return nil, "", fmt.Errorf("firewood: error reading database path: %w", err)
	}

	// Check that the directory exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", fmt.Errorf("firewood: error checking database path: %w", err)
	} else if !info.IsDir() {
		return nil, "", fmt.Errorf("firewood: database path is not a directory: %s", path)
	}

	// Append the filename to the path
	if trieConfig.FileName == "" {
		return nil, "", errors.New("firewood: no filename provided")
	}
	path = filepath.Join(path, trieConfig.FileName)

	// Check if the file exists
	info, err = os.Stat(path)
	exists := false
	if err == nil {
		if info.IsDir() {
			return nil, "", fmt.Errorf("firewood: database path is a directory: %s", path)
		}
		// File exists
		log.Info("Database file found", "path", path)
		exists = true
	}

	// Create the Firewood config from the provided config.
	config := &ffi.Config{
		Create:            !exists,                               // Use any existing file
		NodeCacheEntries:  uint(trieConfig.CleanCacheSize) / 256, // TODO: estimate 256 bytes per node
		Revisions:         trieConfig.Revisions,
		ReadCacheStrategy: trieConfig.ReadCacheStrategy,
		MetricsPort:       trieConfig.MetricsPort,
	}

	return config, path, nil
}

// Scheme returns the scheme of the database.
// This is only used in some weird API calls (TODO: fix that)
// and in StateDB to avoid iterating through deleted storage tries.
func (db *Database) Scheme() string {
	return rawdb.HashScheme
}

// Initialized indicates whether the most recent root of the database
// matches the given root.
func (db *Database) Initialized(root common.Hash) bool {
	// We store the genesis root in the rawdb, so we can check it.
	genesisRoot, err := customrawdb.ReadGenesisRoot(db.ethdb)
	if err != nil {
		log.Error("firewood: error reading genesis root", "error", err)
		return false
	}
	return genesisRoot == root
}

// Update takes a root and a set of keys-values and creates a new proposal.
// It will not be committed until the Commit method is called.
func (db *Database) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	// Create key-value pairs for the nodes in bytes.
	var keys [][]byte
	var values [][]byte

	flattenedNodes := nodes.Flatten()

	for _, nodeset := range flattenedNodes {
		for str, node := range nodeset {
			keys = append(keys, []byte(str))
			values = append(values, node.Blob)
		}
	}

	// Firewood ffi does not accept empty bashes, so if the keys are empty, the root is returned.
	// Empty blocks are not allowed in coreth anyway.
	if len(keys) == 0 {
		return errors.New("firewood: no keys to update")
	}

	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()
	return db.propose(root, parent, block, keys, values)
}

// propose creates a new proposal with the given keys and values.
// If the parent cannot be found, an error will be returned.
// Should only be accessed with the proposal lock held.
func (db *Database) propose(root common.Hash, parent common.Hash, block uint64, keys [][]byte, values [][]byte) error {
	// Check if this proposal already exists. For reorgs, this could occur.
	if existingProposals, ok := db.proposalMap[root]; ok {
		// If the proposal already exists, we can just return.
		for _, existing := range existingProposals {
			if existing.Parent.Root == parent && existing.Block == block {
				return nil
			}
		}
		log.Debug("firewood: proposal already exists, but not at the same parent and block", "root", root.Hex(), "parent", parent.Hex(), "block", block)
	}

	// If parent is root, we should propose from the db.
	// Special case: if we are proposing on top of an empty proposal.
	// This function will never be called in the case that no changes are made,
	// so we must interpret this as a valid potential proposal.
	pCount := 0
	if db.proposalTree.Root == parent {
		p, err := db.fwDisk.Propose(keys, values)
		if err != nil {
			return fmt.Errorf("firewood: error proposing from root %s", parent.Hex())
		}
		pCount++

		// Store the proposal context.
		pContext := &ProposalContext{
			Proposal: p,
			Root:     root,
			Block:    block,
			Parent:   db.proposalTree,
		}
		db.proposalMap[root] = append(db.proposalMap[root], pContext)
		db.proposalTree.Children = append(db.proposalTree.Children, pContext)
	}

	// If the parent is not the root of the database,
	// we need to find all possible parent proposals.
	possibleProposals := db.proposalMap[parent]

	// Find all proposals with the correct parent height.
	// We must create a new proposal for each one, since we don't know which one will be used.
	for _, parentProposal := range possibleProposals {
		// Check if the parent proposal is at the correct height.
		if parentProposal.Block == block-1 {
			p, err := parentProposal.Proposal.Propose(keys, values)
			if err != nil {
				return fmt.Errorf("firewood: error proposing from parent proposal %s", parent.Hex())
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
			pCount++
		}
	}

	// Check the number of proposals actually created.
	if pCount == 0 {
		return fmt.Errorf("firewood: no parent proposal found for %s at height %d", parent.Hex(), block-1)
	} else if pCount > 1 {
		log.Debug("firewood: multiple proposals found for parent", "parent", parent.Hex(), "count", pCount)
	}

	return nil
}

func (db *Database) Close() error {
	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()

	// We don't need to explicitly dereference the proposals, since they will be cleaned up
	// within the firewood close method.
	db.proposalMap = nil
	db.proposalTree.Children = nil
	// Close the database
	return db.fwDisk.Close()
}

// Commit persists a proposal as a revision to the database.
func (db *Database) Commit(root common.Hash, report bool) error {
	// We need to lock the proposal tree to prevent concurrent writes.
	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()
	var pCtx *ProposalContext

	// This should only happen during tests - coreth doesn't allow empty commits.
	// Moreover, any empty change will not call `Update`, so the proposal will not be found.
	if root == db.proposalTree.Root {
		log.Warn("firewood: Commit called with same root as proposal tree root, skipping")
		pCtx = db.proposalTree
		pCtx.Block++ // Increment the block number, since no change is necessary.
		// We must still cleanup the children.
	} else {
		// Find the proposal with the given root.
		// I.e. the proposal in which the parent root is the root of the database.
		// Is this true??? - This is guaranteed to be unique, since it's only height +1 from the disk.
		for _, possible := range db.proposalMap[root] {
			if possible.Parent.Root == db.proposalTree.Root && possible.Parent.Block == db.proposalTree.Block {
				// We found the proposal with the correct parent.
				if pCtx != nil {
					// TODO: Is this possible?
					return fmt.Errorf("firewood: multiple proposals found for %s", root.Hex())
				}
				pCtx = possible
			}
		}
		if pCtx == nil {
			return fmt.Errorf("firewood: proposal not found for %s", root.Hex())
		}

		// Commit the proposal to the database.
		if err := pCtx.Proposal.Commit(); err != nil {
			return fmt.Errorf("firewood: error committing proposal %s", root.Hex())
		}

		// If this is the genesis root, store in rawdb.
		if pCtx.Block == 0 {
			if err := customrawdb.WriteGenesisRoot(db.ethdb, root); err != nil {
				return fmt.Errorf("firewood: error writing genesis root %s: %w", root.Hex(), err)
			}
			log.Info("Persisted genesis root to rawdb", "root", root.Hex())
		}

		if report {
			log.Info("Persisted trie from memory database", "node", root)
		} else {
			log.Debug("Persisted trie from memory database", "node", root)
		}
	}

	// Committing removed the proposal in the backend.
	// We can now remove our reference and promote the context.
	oldChildren := db.proposalTree.Children
	db.proposalTree = pCtx
	db.proposalTree.Parent = nil // We should not index historical revisions here.
	// Remove the proposal from the map.
	db.removeProposalFromMap(pCtx)

	for _, childCtx := range oldChildren {
		// Don't dereference the recently commit proposal.
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
	// This will be implemented in the firewood database eventually.
	return 0, 0
}

// This isn't called anywhere in coreth
func (db *Database) Reference(_ common.Hash, _ common.Hash) {
	log.Error("firewood: Reference not implemented")
}

// Dereference drops a proposal from the database.
// This function is no-op because proposals can only be lazily dereferenced.
// Consider the follwing case:
// Chain 1 has root A and root C
// Chain 2 has root B and root C
// We commit root A, and dereference root B and it's child.
// Root C is Rejected, (which is intended to be 2C) but there's now only one record of root C in the proposal map.
// Thus, we recognize the single root C as the only proposal, and dereference it.
func (db *Database) Dereference(root common.Hash) {
}

// dereference drops a proposal from the database.
// This is a recursive function that will dereference all children first.
// Should only be accessed with the proposal lock held.
// Consumer must not be iterating the proposal map at this root.
// Consumer must remove the context form the tree (done recursively for children of pCtx).
func (db *Database) dereference(pCtx *ProposalContext) error {
	// Base case: if there are children, we need to dereference them first.
	for _, child := range pCtx.Children {
		if err := db.dereference(child); err != nil {
			return fmt.Errorf("firewood: error dereferencing child proposal %s", child.Root.Hex())
		}
	}
	pCtx.Children = nil // We can clear the children now.

	// Remove the proposal from the map.
	db.removeProposalFromMap(pCtx)

	// Drop the proposal in the backend.
	if err := pCtx.Proposal.Drop(); err != nil {
		return fmt.Errorf("firewood: error dropping proposal %s", pCtx.Root.Hex())
	}

	// Don't remove the proposal from the tree.
	// This should be done by the caller.
	return nil
}

// removeProposalFromMap removes the proposal from the proposal map.
// The proposal lock must be held when calling this function.
func (db *Database) removeProposalFromMap(pCtx *ProposalContext) {
	rootList := db.proposalMap[pCtx.Root]
	for i, p := range rootList {
		if p == pCtx { // pointer comparison
			rootList = append(rootList[:i], rootList[i+1:]...) // There could be other proposals with the same root.
			break
		}
	}
	if len(rootList) == 0 {
		// If there are no more proposals with this root, remove it from the map.
		delete(db.proposalMap, pCtx.Root)
	} else {
		db.proposalMap[pCtx.Root] = rootList
	}
}

// Cap iteratively flushes old but still referenced trie nodes until the total
// memory usage goes below the given threshold. The held pre-images accumulated
// up to this point will be flushed in case the size exceeds the threshold.
func (db *Database) Cap(limit common.StorageSize) error {
	// Firewood does not support capping
	return nil
}

// proposalAtRoot returns any proposal at the given root.
// If there are multiple proposals with the same root, it will return the first one.
// If no proposal is found, it will return nil.
func (db *Database) proposalAtRoot(root common.Hash) *ffi.Proposal {
	// Check if the state root corresponds with a proposal.
	proposals, ok := db.proposalMap[root]
	if ok && len(proposals) > 0 {
		// If there are multiple proposals with the same root, we can use the first one.
		if len(proposals) > 1 {
			log.Debug("Multiple proposals found for root", "root", root.Hex(), "count", len(proposals))
		}
		// Use the first proposal
		return proposals[0].Proposal
	}

	// No proposal found
	return nil
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (db *Database) Reader(root common.Hash) (database.Reader, error) {
	// Check if we can currently read the requested root
	var rev *ffi.Revision
	db.proposalLock.RLock()
	defer db.proposalLock.RUnlock()
	prop := db.proposalAtRoot(root)
	if prop == nil {
		var err error
		rev, err = db.fwDisk.Revision(root[:])
		if err != nil {
			return nil, fmt.Errorf("firewood: requested state root %s not found", root.Hex())
		}
	}

	return &reader{db: db, root: root, revision: rev}, nil
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	db       *Database
	root     common.Hash   // The root of the state this reader is reading.
	revision *ffi.Revision // The revision at which this reader is created.
}

// Node retrieves the trie node with the given node hash. No error will be
// returned if the node is not found.
// It defaults to using a revision if available.
func (reader *reader) Node(_ common.Hash, path []byte, _ common.Hash) ([]byte, error) {
	// If we have a revision, we can use it to get the node.
	if reader.revision != nil {
		return reader.revision.Get(path)
	}

	// The most likely path when the revision is nil is that the root is not committed yet.
	reader.db.proposalLock.RLock()
	defer reader.db.proposalLock.RUnlock()
	prop := reader.db.proposalAtRoot(reader.root)
	if prop != nil {
		return prop.Get(path)
	}

	// Assume that the root is now committed and we can use it for the lifetime of the reader.
	rev, err := reader.db.fwDisk.Revision(reader.root[:])
	if err != nil || rev == nil {
		return nil, fmt.Errorf("firewood: requested state root %s not found", reader.root.Hex())
	}
	reader.revision = rev
	return rev.Get(path)
}

// addPendingProposal adds a pending proposal to the database.
func (db *Database) getProposalHash(parentRoot common.Hash, keys, values [][]byte) (common.Hash, error) {
	// This function only reads from existing tracked proposals, so we can use a read lock.
	db.proposalLock.RLock()
	defer db.proposalLock.RUnlock()

	// Check whether if we can propose from the database root.
	var (
		p   IProposal
		err error
	)
	if db.proposalTree.Root == parentRoot {
		// Propose from the database root.
		p, err = db.fwDisk.Propose(keys, values)
		if err != nil {
			return common.Hash{}, fmt.Errorf("firewood: error proposing from root %s", parentRoot.Hex())
		}
	} else {
		// Find any proposal with the given parent root.
		proposals, ok := db.proposalMap[parentRoot]
		if !ok || len(proposals) == 0 {
			return common.Hash{}, fmt.Errorf("firewood: no proposal found for parent root %s", parentRoot.Hex())
		}
		// Use the first proposal with the given parent root.
		rootProposal := proposals[0].Proposal

		p, err = rootProposal.Propose(keys, values)
		if err != nil {
			return common.Hash{}, fmt.Errorf("firewood: error proposing from parent proposal %s", parentRoot.Hex())
		}
	}

	// We succesffuly created a proposal, so we must drop it after use.
	defer p.Drop()

	rootBytes, err := p.Root()
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(rootBytes), nil
}
