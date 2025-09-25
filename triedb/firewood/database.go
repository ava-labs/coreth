// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
)

const (
	dbFileName  = "firewood.db"
	logFileName = "firewood.log"
)

var (
	_ proposable = (*ffi.Database)(nil)
	_ proposable = (*ffi.Proposal)(nil)

	// FFI triedb operation metrics
	ffiProposeCount         = metrics.GetOrRegisterCounter("firewood/triedb/propose/count", nil)
	ffiProposeTimer         = metrics.GetOrRegisterCounter("firewood/triedb/propose/time", nil)
	ffiCommitCount          = metrics.GetOrRegisterCounter("firewood/triedb/commit/count", nil)
	ffiCommitTimer          = metrics.GetOrRegisterCounter("firewood/triedb/commit/time", nil)
	ffiCleanupTimer         = metrics.GetOrRegisterCounter("firewood/triedb/cleanup/time", nil)
	ffiOutstandingProposals = metrics.GetOrRegisterGauge("firewood/triedb/propose/outstanding", nil)

	// FFI Trie operation metrics
	ffiPossibleProposals = metrics.GetOrRegisterGauge("firewood/triedb/propose/possible", nil)
	ffiReadCount         = metrics.GetOrRegisterCounter("firewood/triedb/read/count", nil)
	ffiReadTimer         = metrics.GetOrRegisterCounter("firewood/triedb/read/time", nil)
)

type proposable interface {
	// Propose creates a new proposal from the current state with the given keys and values.
	Propose(keys, values [][]byte) (*ffi.Proposal, error)
}

// ProposalContext represents a proposal in the Firewood database.
// This tracks all outstanding proposals to allow dereferencing upon commit.
type ProposalContext struct {
	Proposal *ffi.Proposal
	Hashes   map[common.Hash]struct{} // All corresponding block hashes
	Root     common.Hash
	Block    uint64
	Parent   *ProposalContext
	Children []*ProposalContext
}

type Config struct {
	ChainDir             string
	CleanCacheSize       int  // Size of the clean cache in bytes
	FreeListCacheEntries uint // Number of free list entries to cache
	Revisions            uint
	ReadCacheStrategy    ffi.CacheStrategy
}

// Note that `FilePath` is not specified, and must always be set by the user.
var Defaults = Config{
	CleanCacheSize:       1024 * 1024, // 1MB
	FreeListCacheEntries: 40_000,
	Revisions:            100,
	ReadCacheStrategy:    ffi.CacheAllReads,
}

func (c Config) BackendConstructor(ethdb.Database) triedb.DBOverride {
	db, err := New(c)
	if err != nil {
		log.Crit("firewood: error creating database", "error", err)
	}
	return db
}

type Database struct {
	fwDisk       *ffi.Database // The underlying Firewood database, used for storing proposals and revisions.
	proposalLock sync.RWMutex
	// proposalMap provides O(1) access by state root to all proposals stored in the proposalTree
	proposalMap map[common.Hash][]*ProposalContext
	// The proposal tree tracks the structure of the current proposals, and which proposals are children of which.
	// This is used to ensure that we can dereference proposals correctly and commit the correct ones
	// in the case of duplicate state roots.
	// The root of the tree is stored here, and represents the top-most layer on disk.
	proposalTree *ProposalContext

	unverifiedProposals []*ProposalContext // Proposals that have been created but not yet verified.
}

// New creates a new Firewood database with the given disk database and configuration.
// Any error during creation will cause the program to exit.
func New(config Config) (*Database, error) {
	if err := validatePath(config.ChainDir); err != nil {
		return nil, err
	}

	// Start the logs prior to opening the database
	logPath := filepath.Join(config.ChainDir, logFileName)
	if err := ffi.StartLogs(&ffi.LogConfig{Path: logPath}); err != nil {
		// This shouldn't be a fatal error, as this can only be called once per thread.
		// Specifically, this will return an error in unit tests.
		log.Error("firewood: error starting logs", "error", err)
	}

	dbPath := filepath.Join(config.ChainDir, dbFileName)
	fw, err := ffi.New(dbPath, &ffi.Config{
		NodeCacheEntries:     uint(config.CleanCacheSize) / 256, // TODO: estimate 256 bytes per node
		FreeListCacheEntries: config.FreeListCacheEntries,
		Revisions:            config.Revisions,
		ReadCacheStrategy:    config.ReadCacheStrategy,
	})
	if err != nil {
		return nil, err
	}

	currentRoot, err := fw.Root()
	if err != nil {
		return nil, err
	}

	return &Database{
		fwDisk:      fw,
		proposalMap: make(map[common.Hash][]*ProposalContext),
		proposalTree: &ProposalContext{
			Root: common.Hash(currentRoot),
			Hashes: map[common.Hash]struct{}{
				{}: {}, // genesis block has empty hash
			},
		},
	}, nil
}

func validatePath(dir string) error {
	if dir == "" {
		return errors.New("chain data directory must be set")
	}

	// Check that the directory exists
	switch info, err := os.Stat(dir); {
	case os.IsNotExist(err):
		log.Info("Database directory not found, creating", "path", dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("error creating database directory: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("error checking database directory: %w", err)
	case !info.IsDir():
		return fmt.Errorf("database directory path is not a directory: %s", dir)
	}

	return nil
}

// Scheme returns the scheme of the database.
// This is only used in some API calls
// and in StateDB to avoid iterating through deleted storage tries.
// WARNING: If cherry-picking anything from upstream that uses this,
// it must be overwritten to use something like:
// `_, ok := db.(*Database); if !ok { return "" }`
// to recognize the Firewood database.
func (*Database) Scheme() string {
	return rawdb.HashScheme
}

// Initialized checks whether a non-empty genesis block has been written.
func (db *Database) Initialized(common.Hash) bool {
	rootBytes, err := db.fwDisk.Root()
	if err != nil {
		log.Error("firewood: error getting current root", "error", err)
		return false
	}
	root := common.BytesToHash(rootBytes)
	// If the current root isn't empty, then unless the database is empty, we have a genesis block recorded.
	return root != types.EmptyRootHash
}

// Update takes a root and a set of keys-values and creates a new proposal.
// It will not be committed until the Commit method is called.
// This function should be called even if there are no changes to the state to ensure proper tracking of block hashes.
func (db *Database) Update(root common.Hash, parentRoot common.Hash, block uint64, nodes *trienode.MergedNodeSet, _ *triestate.Set, opts ...stateconf.TrieDBUpdateOption) error {
	// We require block hashes to be provided for all blocks in production.
	// However, many tests cannot reasonably provide a block hash for genesis, so we allow it to be omitted.
	parentHash, hash, ok := stateconf.ExtractTrieDBUpdatePayload(opts...)
	if !ok {
		log.Error("firewood: no block hash provided for block %d", block)
	}

	// The rest of the operations except key-value arranging must occur with a lock
	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()

	defer func() {
		// Drop any unused proposals passed from account trie's Commit
		// This will only occur on the error case.
		for _, pCtx := range db.unverifiedProposals {
			if dropErr := pCtx.Proposal.Drop(); dropErr != nil {
				log.Error("firewood: error dropping unverified proposal", "root", pCtx.Root.Hex(), "error", dropErr)
			}
			ffiPossibleProposals.Dec(1)
		}
		db.unverifiedProposals = nil
	}()

	// Check if this proposal already exists.
	// During reorgs, we may have already created this proposal.
	// Additionally, we may have already created this proposal with a different block hash.
	if existingProposals, ok := db.proposalMap[root]; ok {
		for _, existing := range existingProposals {
			// If the block hash is already tracked, we can skip proposing this again.
			if _, exists := existing.Hashes[hash]; exists {
				log.Debug("firewood: proposal already exists", "root", root.Hex(), "parent", parentRoot.Hex(), "block", block, "hash", hash.Hex())
				return nil
			}
			// We already have this proposal, but should create a new context with the correct hash.
			// This solves the case of a unique block hash, but the same underlying proposal.
			if _, exists := existing.Parent.Hashes[parentHash]; exists {
				log.Debug("firewood: proposal already exists, updating hash", "root", root.Hex(), "parent", parentRoot.Hex(), "block", block, "hash", hash.Hex())
				existing.Hashes[hash] = struct{}{}
				return nil
			}
		}
	}

	// We must use one of the unverified proposals if it exists.
	var pCtx *ProposalContext
	for _, possible := range db.unverifiedProposals {
		if _, ok := possible.Parent.Hashes[parentHash]; ok {
			pCtx = possible
			break
		}
	}
	if pCtx == nil {
		// Edge case: first set of proposals before `Commit`, or empty genesis block
		// Neither `Update` nor `Commit` is called for genesis, so we can accept a proposal with parentHash of empty.
		for _, possible := range db.unverifiedProposals {
			if possible.Parent.Hashes[common.Hash{}] == struct{}{} {
				pCtx = possible
				pCtx.Block = block
				pCtx.Parent.Hashes[parentHash] = struct{}{}
				break
			}
		}
		if pCtx == nil {
			return fmt.Errorf("firewood: no unverified proposal found for block %d, root %s, hash %s", block, root.Hex(), hash.Hex())
		}
	}

	// Verify that the proposal context matches what we expect.
	switch {
	case pCtx.Root != root:
		return fmt.Errorf("firewood: proposal root mismatch, expected %s, got %s", root.Hex(), pCtx.Root.Hex())
	case pCtx.Parent.Root != parentRoot:
		return fmt.Errorf("firewood: proposal parent root mismatch, expected %s, got %s", parentRoot.Hex(), pCtx.Parent.Root.Hex())
	case pCtx.Block != block:
		return fmt.Errorf("firewood: proposal block mismatch, expected %d, got %d", block, pCtx.Block)
	}

	// Track the proposal context in the tree and map.
	pCtx.Parent.Children = append(pCtx.Parent.Children, pCtx)
	db.proposalMap[root] = append(db.proposalMap[root], pCtx)
	pCtx.Hashes[hash] = struct{}{}
	ffiOutstandingProposals.Inc(1)

	// Clean all other unverified proposals that were not used.
	for _, unverified := range db.unverifiedProposals {
		if unverified != pCtx {
			if dropErr := unverified.Proposal.Drop(); dropErr != nil {
				log.Error("firewood: error dropping unverified proposal", "root", unverified.Root.Hex(), "error", dropErr)
			}
		}
		ffiPossibleProposals.Dec(1)
	}
	// Clear the unverified proposals slice avoiding double freeing.
	db.unverifiedProposals = nil
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

	// On success, we should dereference all children of the committed proposal.
	defer func() {
		if pCtx != nil {
			db.cleanupCommittedProposal(pCtx)
		}
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
		return fmt.Errorf("firewood: committable proposal not found for %s", root.Hex())
	}

	start := time.Now()
	// Commit the proposal to the database.
	if commitErr := pCtx.Proposal.Commit(); commitErr != nil {
		return fmt.Errorf("firewood: error committing proposal %s: %w", root.Hex(), commitErr)
	}
	ffiCommitCount.Inc(1)
	ffiCommitTimer.Inc(time.Since(start).Milliseconds())
	ffiOutstandingProposals.Dec(1)

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
func (*Database) Size() (common.StorageSize, common.StorageSize) {
	return 0, 0
}

// This isn't called anywhere in coreth
func (*Database) Reference(common.Hash, common.Hash) {
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
func (*Database) Dereference(common.Hash) {
}

// Firewood does not support this.
func (*Database) Cap(common.StorageSize) error {
	return nil
}

func (db *Database) Close() error {
	db.proposalLock.Lock()
	defer db.proposalLock.Unlock()

	// before closing, we must deference any outstanding proposals to free the
	// memory owned by firewood (outside of go's memory management)
	for _, pCtx := range db.proposalTree.Children {
		db.dereference(pCtx)
	}

	db.proposalMap = nil
	db.proposalTree.Children = nil

	// Close the database
	return db.fwDisk.Close()
}

// createProposal creates a new proposal from the given layer
// If there are no changes, it will return nil.
func (db *Database) createProposal(parent *ProposalContext, keys, values [][]byte) (pCtx *ProposalContext, err error) {
	var (
		start = time.Now()
		p     *ffi.Proposal
	)
	// If there's an error after creating the proposal, we must drop it.
	defer func() {
		if err != nil && p != nil {
			if dropErr := p.Drop(); dropErr != nil {
				// We should still return the original error.
				log.Error("firewood: error dropping proposal after error", "root", pCtx.Root.Hex(), "error", dropErr)
			}
			pCtx = nil
		}
	}()

	if parent.Proposal == nil {
		p, err = db.fwDisk.Propose(keys, values)
	} else {
		p, err = parent.Proposal.Propose(keys, values)
	}
	if err != nil {
		return nil, fmt.Errorf("firewood: unable to create proposal from parent root %s: %w", parent.Root.Hex(), err)
	}
	ffiProposeCount.Inc(1)
	ffiProposeTimer.Inc(time.Since(start).Milliseconds())
	ffiPossibleProposals.Inc(1)

	currentRootBytes, err := p.Root()
	if err != nil {
		return nil, fmt.Errorf("firewood: error getting root of proposals: %w", err)
	}
	root := common.BytesToHash(currentRootBytes)

	// Edge case: genesis block
	block := parent.Block + 1
	if _, ok := parent.Hashes[common.Hash{}]; ok && parent.Root == types.EmptyRootHash {
		block = 0
	}

	pCtx = &ProposalContext{
		Proposal: p,
		Root:     root,
		Hashes:   make(map[common.Hash]struct{}),
		Parent:   parent,
		Block:    block,
	}
	return pCtx, nil
}

// cleanupCommittedProposal dereferences the proposal and removes it from the proposal map.
// It also recursively dereferences all children of the proposal.
func (db *Database) cleanupCommittedProposal(pCtx *ProposalContext) {
	start := time.Now()
	oldChildren := db.proposalTree.Children
	db.proposalTree = pCtx
	db.proposalTree.Parent = nil
	db.proposalTree.Proposal = nil // The proposals has been committed.

	db.removeProposalFromMap(pCtx)

	for _, childCtx := range oldChildren {
		// Don't dereference the recently commit proposal.
		if childCtx != pCtx {
			db.dereference(childCtx)
		}
	}
	ffiCleanupTimer.Inc(time.Since(start).Milliseconds())
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
	if err := pCtx.Proposal.Drop(); err != nil {
		log.Error("firewood: error dropping proposal", "root", pCtx.Root.Hex(), "error", err)
	}
	ffiOutstandingProposals.Dec(1)
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

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (db *Database) Reader(root common.Hash) (database.Reader, error) {
	if _, err := db.fwDisk.GetFromRoot(root.Bytes(), []byte{}); err != nil {
		return nil, fmt.Errorf("firewood: unable to retrieve from root %s: %w", root.Hex(), err)
	}
	return &reader{db: db, root: root}, nil
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	db   *Database
	root common.Hash // The root of the state this reader is reading.
}

// Node retrieves the trie node with the given node hash. No error will be
// returned if the node is not found.
func (reader *reader) Node(_ common.Hash, path []byte, _ common.Hash) ([]byte, error) {
	// This function relies on Firewood's internal locking to ensure concurrent reads are safe.
	// This is safe even if a proposal is being committed concurrently.
	start := time.Now()
	result, err := reader.db.fwDisk.GetFromRoot(reader.root.Bytes(), path)
	if metrics.EnabledExpensive {
		ffiReadCount.Inc(1)
		ffiReadTimer.Inc(time.Since(start).Milliseconds())
	}
	return result, err
}

// getProposalHash calculates the hash if the set of keys and values are
// proposed from the given parent root.
func (db *Database) getProposalHash(parentRoot common.Hash, keys, values [][]byte) (root common.Hash, proposals []*ProposalContext, err error) {
	if len(keys) != len(values) {
		return common.Hash{}, nil, fmt.Errorf("firewood: keys and values must have the same length, got %d keys and %d values", len(keys), len(values))
	}

	// This function only reads from existing tracked proposals, so we can use a read lock.
	db.proposalLock.RLock()
	defer db.proposalLock.RUnlock()

	defer func() {
		// If any error is encountered, free all proposals created in this function.
		if err != nil {
			for _, p := range proposals {
				if dropErr := p.Proposal.Drop(); dropErr != nil {
					// Report the error, but we should still return the original error.
					log.Error("firewood: error dropping proposal after error", "root", p.Root.Hex(), "error", dropErr)
				}
				ffiPossibleProposals.Dec(1)
			}
			proposals = nil
			root = common.Hash{}
		}
	}()

	// TODO: metrics
	// start := time.Now()
	if db.proposalTree.Root == parentRoot {
		// Propose from the database root.
		p, err := db.createProposal(db.proposalTree, keys, values)
		if err != nil {
			return common.Hash{}, proposals, fmt.Errorf("firewood: error proposing from root %s: %w", parentRoot.Hex(), err)
		}
		proposals = append(proposals, p)
	}

	// Find any proposal with the given parent root.
	// Since we are only using the proposal to find the root hash,
	// we can use the first proposal found.
	for _, parent := range db.proposalMap[parentRoot] {
		p, err := db.createProposal(parent, keys, values)
		if err != nil {
			return common.Hash{}, proposals, fmt.Errorf("firewood: error proposing from parent root %s: %w", parentRoot.Hex(), err)
		}
		proposals = append(proposals, p)
	}

	if len(proposals) == 0 {
		return common.Hash{}, nil, fmt.Errorf("firewood: no proposals found with parent root %s", parentRoot.Hex())
	}

	// Get the root of the first proposal - they should all match.
	rootBytes, err := proposals[0].Proposal.Root()
	if err != nil {
		return common.Hash{}, proposals, fmt.Errorf("firewood: error getting root of proposal: %w", err)
	}
	root = common.BytesToHash(rootBytes)

	return root, proposals, nil
}
