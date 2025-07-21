// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/libevm/common"

	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	syncclient "github.com/ava-labs/coreth/sync/client"
)

const (
	// MinNumWorkers is the minimum number of worker goroutines to use for atomic trie syncing.
	MinNumWorkers = 1
)

var (
	// ErrWaitBeforeStart is returned when Wait() is called before Start().
	ErrWaitBeforeStart = errors.New("Wait() called before Start() - call Start() first")

	// ErrTooFewWorkers is returned when numWorkers is below the minimum.
	ErrTooFewWorkers = errors.New("numWorkers below minimum")

	// ErrTooManyWorkers is returned when numWorkers exceeds the maximum.
	ErrTooManyWorkers = errors.New("numWorkers above maximum")

	// Cache CPU count to avoid repeated calls to runtime.NumCPU().
	cpuCount = runtime.NumCPU()

	// maxWorkers keeps track of the maximum number of worker goroutines to use for atomic trie syncing.
	// For I/O bound work like network syncing, we can be more aggressive than CPU-bound work.
	// This allows up to 2x CPU cores for I/O bound work, but caps at 64 for very large systems.
	// Rationale:
	// - I/O bound work benefits from more goroutines than CPU cores.
	// - 2x CPU cores provides good parallelism without overwhelming the system.
	// - Cap of 64 prevents excessive resource usage on very large systems.
	maxWorkers = min(cpuCount*2, 64)

	// defaultWorkers keeps track of the optimal number of worker goroutines for atomic trie syncing
	// based on available CPU cores, with sensible bounds.
	// Note: These are goroutines, not OS threads. The Go runtime scheduler will distribute
	// them efficiently across the available OS threads (GOMAXPROCS).
	// Rationale:
	// - 75% of CPU cores provides good parallelism while leaving headroom for other operations.
	// - This balances performance with system resource usage.
	// - Bounded by MinNumWorkers and MaxNumWorkers for safety.
	defaultWorkers = max(MinNumWorkers, min((cpuCount*3)/4, maxWorkers))
)

// Config holds the configuration for creating a new atomic syncer.
type Config struct {
	// Client is the leaf client used to fetch data from the network.
	Client syncclient.LeafClient

	// Database is the version database for storing synced data.
	Database *versiondb.Database

	// AtomicTrie is the atomic trie to sync into.
	AtomicTrie *atomicstate.AtomicTrie

	// TargetRoot is the root hash of the trie to sync to.
	TargetRoot common.Hash

	// TargetHeight is the target block height to sync to.
	TargetHeight uint64

	// RequestSize is the maximum number of leaves to request in a single network call.
	RequestSize uint16

	// NumWorkers is the number of worker goroutines to use for syncing.
	// If not set, DefaultNumWorkers() will be used.
	NumWorkers int
}

// MaxNumWorkers returns the maximum number of worker goroutines to use for atomic trie syncing.
func MaxNumWorkers() int {
	return maxWorkers
}

// DefaultNumWorkers returns the optimal number of worker goroutines for atomic trie syncing
// based on available CPU cores, with sensible bounds.
func DefaultNumWorkers() int {
	return defaultWorkers
}

// Validate checks if the configuration is valid and returns an error if not.
func (c *Config) Validate() error {
	// Use default workers if not specified
	numWorkers := c.NumWorkers
	if numWorkers == 0 {
		numWorkers = defaultWorkers
	}

	// Validate worker count using cached values.
	if numWorkers < MinNumWorkers {
		return fmt.Errorf("%w: %d (minimum: %d)", ErrTooFewWorkers, numWorkers, MinNumWorkers)
	}

	if numWorkers > maxWorkers {
		return fmt.Errorf("%w: %d (maximum: %d)", ErrTooManyWorkers, numWorkers, maxWorkers)
	}

	return nil
}
