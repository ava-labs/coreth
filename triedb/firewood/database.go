// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"

	ffi "github.com/ava-labs/firewood-go-ethhash/ffi"
)

var (
	_ proposable = (*ffi.Database)(nil)
	_ proposable = (*ffi.Proposal)(nil)
)

type proposable interface {
	// Propose creates a new proposal from the current state with the given keys and values.
	Propose(keys, values [][]byte) (*ffi.Proposal, error)
}

// ProposalContext represents a proposal in the Firewood database.
// This tracks all outstanding proposals to allow dereferencing upon commit.
type ProposalContext struct {
	Proposal   *ffi.Proposal
	Hashes     map[common.Hash]struct{} // All corresponding block hashes
	ParentHash common.Hash              // The parent block hash, if known
	Root       common.Hash
	Block      uint64
	Parent     *ProposalContext
	Children   []*ProposalContext
}

type Config struct {
	FileName          string
	CleanCacheSize    int // Size of the clean cache in bytes
	Revisions         uint
	ReadCacheStrategy ffi.CacheStrategy
	MetricsPort       uint16
}

var Defaults = &Config{
	FileName:          "firewood",
	CleanCacheSize:    1024 * 1024, // 1MB
	Revisions:         100,
	ReadCacheStrategy: ffi.CacheAllReads,
	MetricsPort:       0, // Disable metrics by default
}

// Must take reference to allow closure - reuse any existing database for the same config.
func (c Config) BackendConstructor(diskdb ethdb.Database) triedb.DBOverride {
	return New(diskdb, &c)
}

type Database struct {
	fwDisk *ffi.Database  // The underlying Firewood database, used for storing proposals and revisions.
	ethdb  ethdb.Database // The underlying disk database, used for storing genesis and the path.

	proposalLock sync.RWMutex
	// proposalMap provides O(1) access to all proposals stored in the proposalTree
	proposalMap map[common.Hash][]*ProposalContext
	// The proposal tree tracks the structure of the current proposals, and which proposals are children of which.
	// This is used to ensure that we can dereference proposals correctly and commit the correct ones
	// in the case of duplicate state roots.
	// The root of the tree is stored here, and represents the top-most layer on disk.
	proposalTree *ProposalContext
}

// New creates a new Firewood database with the given disk database and configuration.
// Any error during creation will cause the program to exit.
func New(diskdb ethdb.Database, config *Config) *Database {
	if config == nil {
		config = Defaults
	}

	fwConfig, path, err := validateConfig(diskdb, config)
	if err != nil {
		log.Crit("firewood: error validating config", "error", err)
	}

	fw, err := ffi.New(path, fwConfig)
	if err != nil {
		log.Crit("firewood: error creating firewood database", "error", err)
	}

	currentRoot, err := fw.Root()
	if err != nil {
		log.Crit("firewood: error getting current root", "error", err)
	}

	return &Database{
		fwDisk:      fw,
		ethdb:       diskdb,
		proposalMap: make(map[common.Hash][]*ProposalContext),
		proposalTree: &ProposalContext{
			Root: common.Hash(currentRoot),
		},
	}
}

func validateConfig(diskdb ethdb.Database, trieConfig *Config) (*ffi.Config, string, error) {
	// Get the path from the database
	path, err := customrawdb.ReadChainDataPath(diskdb)
	if err != nil {
		return nil, "", fmt.Errorf("unable to read database path: %w", err)
	}

	// Check that the directory exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", fmt.Errorf("error checking database path: %w", err)
	} else if !info.IsDir() {
		return nil, "", fmt.Errorf("database path is not a directory: %s", path)
	}

	// Append the filename to the path
	if trieConfig.FileName == "" {
		return nil, "", errors.New("no filename provided")
	}
	path = filepath.Join(path, trieConfig.FileName)

	// Check if the file exists
	info, err = os.Stat(path)
	exists := false
	if err == nil {
		if info.IsDir() {
			return nil, "", fmt.Errorf("database file path is a directory: %s", path)
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
// This is only used in some API calls
// and in StateDB to avoid iterating through deleted storage tries.
func (db *Database) Scheme() string {
	return rawdb.HashScheme
}

// Initialized indicates whether the root provided is the genesis root of the database.
func (db *Database) Initialized(root common.Hash) bool {
	// We store the genesis root in the rawdb, so we can check it.
	genesisRoot, err := customrawdb.ReadFirewoodGenesisRoot(db.ethdb)
	if err != nil {
		log.Error("firewood: error reading genesis root", "error", err)
		return false
	}
	return genesisRoot == root
}

// Update takes a root and a set of keys-values and creates a new proposal.
// It will not be committed until the Commit method is called.
// This function should be called even if there are no changes to the state to ensure proper tracking of block hashes.
func (db *Database) Update(root common.Hash, parentRoot common.Hash, block uint64, nodes *trienode.MergedNodeSet, _ *triestate.Set, opts ...stateconf.TrieDBUpdateOption) error {
	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()

	// We require block hashes to be provided for all blocks except the genesis block.
	parentHash, hash, ok := stateconf.ExtractTrieDBUpdatePayload(opts...)
	if !ok && block >= 1 {
		return fmt.Errorf("firewood: no block hash provided for block %d", block)
	}

	// Check if this proposal already exists.
	// During reorgs, we may have already created this proposal.
	// Additinoally, we may have already created this proposal with a different block hash.
	if existingProposals, ok := db.proposalMap[root]; ok {
		for _, existing := range existingProposals {
			// If the block hash is already tracked, we can skip proposing this again.
			if _, exists := existing.Hashes[hash]; exists {
				log.Debug("firewood: proposal already exists", "root", root.Hex(), "parent", parentRoot.Hex(), "block", block, "hash", hash.Hex())
				return nil
			}
			// We already have this proposal, but should create a new context with the correct hash.
			// This solves the case of a unique block hash, but the same underlying proposal.
			if existing.ParentHash == parentHash && existing.Root == root && existing.Block == block {
				log.Debug("firewood: proposal already exists, updating hash", "root", root.Hex(), "parent", parentRoot.Hex(), "block", block, "hash", hash.Hex())
				existing.Hashes[hash] = struct{}{}
				return nil
			}
		}
	}

	keys, values := arrangeKeyValuePairs(nodes) // may return nil, nil if no changes
	return db.propose(root, parentRoot, hash, parentHash, block, keys, values)
}

// propose creates a new proposal for every possible parent with the given keys and values.
// If the parent cannot be found, an error will be returned.
//
// To avoid having to create a new proposal for each valid state root, the block hashes are
// provided to ensure uniqueness. When this method is called, we can guarantee that the proposalContext
// must be created and tracked. There are two edge cases for using the block hash:
//
// 1. For the genesis block, we will not yet know the block hash, so we cannot store it.
// 2. For block height 1, since we didn't store the genesis block hash, we will not have a parent hash.
//
// Note that if the keys and values are empty, this will not create a proposal in Firewood, but only provide tracking.
//
// Should only be accessed with the proposal lock held.
func (db *Database) propose(root common.Hash, parentRoot common.Hash, hash common.Hash, parentHash common.Hash, block uint64, keys [][]byte, values [][]byte) error {
	// Find the parent proposal with the correct hash.
	// We assume the number of proposals at a given root is small, so we can iterate through them.
	for _, parentProposal := range db.proposalMap[parentRoot] {
		// If we know this proposal cannot be the parent, we can skip it.
		// Since the only possible block that won't have a parent hash is block 1,
		// and that will always be proposed from the database root,
		// we can guarantee that the parent hash will be present in one of the proposals.
		if _, exists := parentProposal.Hashes[parentHash]; !exists {
			continue
		}
		log.Debug("firewood: proposing from parent proposal", "parent", parentProposal.Root.Hex(), "root", root.Hex(), "height", block)
		p, err := db.createProposal(parentProposal.Proposal, root, keys, values)
		if err != nil {
			return err
		}
		pCtx := &ProposalContext{
			Proposal:   p,
			Hashes:     map[common.Hash]struct{}{hash: {}},
			ParentHash: parentHash,
			Root:       root,
			Block:      block,
			Parent:     parentProposal,
		}

		db.proposalMap[root] = append(db.proposalMap[root], pCtx)
		parentProposal.Children = append(parentProposal.Children, pCtx)
		return nil
	}

	// Since we were unable to find a parent proposal with the given parent hash,
	// we must create a new proposal from the database root.
	// Note that if this is block <= 1, we will not yet be tracking parent hashes, so we can skip this check.
	if _, exists := db.proposalTree.Hashes[parentHash]; block > 1 && !exists {
		return fmt.Errorf("firewood: parent hash %s not found for block %s at height %d", parentHash.Hex(), hash.Hex(), block)
	} else if block <= 1 && db.proposalTree.Root != parentRoot {
		return fmt.Errorf("firewood: parent root %s does not match proposal tree root %s for root %s at height %d", parentRoot.Hex(), db.proposalTree.Root.Hex(), root.Hex(), block)
	}

	log.Debug("firewood: proposing from database root", "root", root.Hex(), "height", block)
	p, err := db.createProposal(db.fwDisk, root, keys, values)
	if err != nil {
		return err
	}
	pCtx := &ProposalContext{
		Proposal:   p,
		Hashes:     map[common.Hash]struct{}{hash: {}}, // This may be common.Hash{} for genesis blocks.
		ParentHash: parentHash,
		Root:       root,
		Block:      block,
		Parent:     db.proposalTree,
	}
	db.proposalMap[root] = append(db.proposalMap[root], pCtx)
	db.proposalTree.Children = append(db.proposalTree.Children, pCtx)

	return nil
}

// Commit persists a proposal as a revision to the database.
//
// Any time this is called, we expect either:
//  1. The root is the same as the current root of the database (empty block during bootstrapping)
//  2. We have created a valid propsal with that root, and it is of height +1 above the proposal tree root.
//     Additionally, this should be unique.
//
// Afterward, we know that no other proposal at this height can be committed, so we can dereference all
// children in the the other branches of the proposal tree.
func (db *Database) Commit(root common.Hash, report bool) (err error) {
	// We need to lock the proposal tree to prevent concurrent writes.
	var pCtx *ProposalContext
	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()

	// On success, we should persist the genesis root as necessary, and dereference all children
	// of the committed proposal.
	defer func() {
		if err != nil {
			return
		}
		// If this is the genesis root, store in rawdb.
		if pCtx.Block == 0 {
			if writeErr := customrawdb.WriteFirewoodGenesisRoot(db.ethdb, root); writeErr != nil {
				err = fmt.Errorf("firewood: error writing genesis root %s: %w", root.Hex(), writeErr)
				return
			}
			log.Info("Persisted genesis root in firewood", "root", root.Hex())
		}

		db.cleanupCommittedProposal(pCtx)
	}()

	// Find the proposal with the given root.
	for _, possible := range db.proposalMap[root] {
		if possible.Parent.Root == db.proposalTree.Root && possible.Parent.Block == db.proposalTree.Block {
			// We found the proposal with the correct parent.
			if pCtx != nil {
				// This should never happen, as we ensure that we don't create duplicate proposals in `propose`.
				return fmt.Errorf("firewood: multiple proposals found for %s", root.Hex())
			}
			pCtx = possible
		}
	}
	if pCtx == nil {
		return fmt.Errorf("firewood: proposal not found for %s", root.Hex())
	}

	// Commit the proposal to the database.
	if commitErr := pCtx.Proposal.Commit(); commitErr != nil {
		return fmt.Errorf("firewood: error committing proposal %s: %w", root.Hex(), commitErr)
	}

	// Assert that the root of the database matches the committed proposal root.
	currentRootBytes, err := db.fwDisk.Root()
	if err != nil {
		return fmt.Errorf("firewood: error getting current root after commit: %w", err)
	}
	currentRoot := common.BytesToHash(currentRootBytes)
	if currentRoot != root {
		return fmt.Errorf("firewood: current root %s does not match expected root %s", currentRoot.Hex(), root.Hex())
	}

	if report {
		log.Info("Persisted proposal to firewood database", "root", root)
	} else {
		log.Debug("Persisted proposal to firewood database", "root", root)
	}
	return nil
}

// Size returns the storage size of diff layer nodes above the persistent disk
// layer and the dirty nodes buffered within the disk layer
// Only used for metrics and Commit intervals in APIs.
// This will be implemented in the firewood database eventually.
// Currently, Firewood stores all revisions in disk and proposals in memory.
func (db *Database) Size() (common.StorageSize, common.StorageSize) {
	return 0, 0
}

// This isn't called anywhere in coreth
func (db *Database) Reference(_ common.Hash, _ common.Hash) {
	log.Error("firewood: Reference not implemented")
}

// Dereference drops a proposal from the database.
// This function is no-op because unused proposals are dereferenced when no longer valid.
// We cannot dereference at this call. Consider the following case:
// Chain 1 has root A and root C
// Chain 2 has root B and root C
// We commit root A, and immediately dereference root B and its child.
// Root C is Rejected, (which is intended to be 2C) but there's now only one record of root C in the proposal map.
// Thus, we recognize the single root C as the only proposal, and dereference it.
func (db *Database) Dereference(root common.Hash) {
}

// Firewood does not support this.
func (db *Database) Cap(limit common.StorageSize) error {
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

// createProposal creates a new proposal from the given layer
// If there are no changes, it will return nil.
func (db *Database) createProposal(layer proposable, root common.Hash, keys, values [][]byte) (*ffi.Proposal, error) {
	if len(keys) != len(values) {
		return nil, fmt.Errorf("firewood: keys and values must have the same length, got %d keys and %d values", len(keys), len(values))
	}

	p, err := layer.Propose(keys, values)
	if err != nil {
		return nil, fmt.Errorf("firewood: unable to create proposal for root %s: %w", root.Hex(), err)
	}

	currentRootBytes, err := p.Root()
	if err != nil {
		return nil, fmt.Errorf("firewood: error getting root of proposal %s: %w", root, err)
	}
	currentRoot := common.BytesToHash(currentRootBytes)
	if root != currentRoot {
		return nil, fmt.Errorf("firewood: proposed root %s does not match expected root %s", currentRoot.Hex(), root.Hex())
	}

	// Store the proposal context.
	return p, nil
}

// cleanupCommittedProposal dereferences the proposal and removes it from the proposal map.
// It also recursively dereferences all children of the proposal.
func (db *Database) cleanupCommittedProposal(pCtx *ProposalContext) {
	oldChildren := db.proposalTree.Children
	db.proposalTree = pCtx
	db.proposalTree.Parent = nil

	db.removeProposalFromMap(pCtx)

	for _, childCtx := range oldChildren {
		// Don't dereference the recently commit proposal.
		// There is no chance that we have to "reparent" the proposal.
		if childCtx != pCtx {
			db.dereference(childCtx)
		}
	}
}

// Internally removes all references of the proposal from the database.
// Should only be accessed with the proposal lock held.
// Consumer must not be iterating the proposal map at this root.
func (db *Database) dereference(pCtx *ProposalContext) {
	// Base case: if there are children, we need to dereference them as well.
	for _, child := range pCtx.Children {
		db.dereference(child)
	}
	pCtx.Children = nil

	// Remove the proposal from the map.
	db.removeProposalFromMap(pCtx)

	// Drop the proposal in the backend.
	// ignore any error
	if err := pCtx.Proposal.Drop(); err != nil {
		log.Error("firewood: error dropping proposal", "root", pCtx.Root.Hex(), "error", err)
	}
}

// removeProposalFromMap removes the proposal from the proposal map.
// The proposal lock must be held when calling this function.
func (db *Database) removeProposalFromMap(pCtx *ProposalContext) {
	rootList := db.proposalMap[pCtx.Root]
	for i, p := range rootList {
		if p == pCtx { // pointer comparison - guaranteed to be unique
			rootList[i] = rootList[len(rootList)-1]
			rootList[len(rootList)-1] = nil
			rootList = rootList[:len(rootList)-1]
			break
		}
	}
	if len(rootList) == 0 {
		delete(db.proposalMap, pCtx.Root)
	} else {
		db.proposalMap[pCtx.Root] = rootList
	}
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
		return proposals[0].Proposal
	}

	// No proposal found
	return nil
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (db *Database) Reader(root common.Hash) (database.Reader, error) {
	db.proposalLock.RLock()
	defer db.proposalLock.RUnlock()
	// Check if we can currently read the requested root
	var rev *ffi.Revision
	prop := db.proposalAtRoot(root)
	if prop == nil {
		var err error
		rev, err = db.fwDisk.Revision(root.Bytes())
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
	rev, err := reader.db.fwDisk.Revision(reader.root.Bytes())
	if err != nil || rev == nil {
		return nil, fmt.Errorf("firewood: requested state root %s not found", reader.root.Hex())
	}
	reader.revision = rev
	return rev.Get(path)
}

// getProposalHash calculates the hash if the set of keys and values are
// proposed from the given parent root.
func (db *Database) getProposalHash(parentRoot common.Hash, keys, values [][]byte) (common.Hash, error) {
	// This function only reads from existing tracked proposals, so we can use a read lock.
	db.proposalLock.RLock()
	defer db.proposalLock.RUnlock()

	var (
		p   *ffi.Proposal
		err error
	)
	if db.proposalTree.Root == parentRoot {
		// Propose from the database root.
		p, err = db.fwDisk.Propose(keys, values)
		if err != nil {
			return common.Hash{}, fmt.Errorf("firewood: error proposing from root %s: %v", parentRoot.Hex(), err)
		}
	} else {
		// Find any proposal with the given parent root.
		// Since we are only using the proposal to find the root hash,
		// we can use the first proposal found.
		proposals, ok := db.proposalMap[parentRoot]
		if !ok || len(proposals) == 0 {
			return common.Hash{}, fmt.Errorf("firewood: no proposal found for parent root %s", parentRoot.Hex())
		}
		rootProposal := proposals[0].Proposal

		p, err = rootProposal.Propose(keys, values)
		if err != nil {
			return common.Hash{}, fmt.Errorf("firewood: error proposing from parent proposal %s: %v", parentRoot.Hex(), err)
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

func arrangeKeyValuePairs(nodes *trienode.MergedNodeSet) ([][]byte, [][]byte) {
	if nodes == nil {
		return nil, nil // No changes to propose
	}
	// Create key-value pairs for the nodes in bytes.
	var (
		acctKeys      [][]byte
		acctValues    [][]byte
		storageKeys   [][]byte
		storageValues [][]byte
	)

	flattenedNodes := nodes.Flatten()

	for _, nodeset := range flattenedNodes {
		for str, node := range nodeset {
			if len(str) == common.HashLength {
				// This is an account node.
				acctKeys = append(acctKeys, []byte(str))
				acctValues = append(acctValues, node.Blob)
			} else {
				storageKeys = append(storageKeys, []byte(str))
				storageValues = append(storageValues, node.Blob)
			}
		}
	}

	// We need to do all storage operations first, so prefix-deletion works for accounts.
	keys := append(storageKeys, acctKeys...)
	values := append(storageValues, acctValues...)
	return keys, values
}
