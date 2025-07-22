// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/libevm/common"

	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	syncclient "github.com/ava-labs/coreth/sync/client"
)

var (
	// ErrWaitBeforeStart is returned when Wait() is called before Start().
	ErrWaitBeforeStart = errors.New("Wait() called before Start() - call Start() first")

	MinNumWorkers     = 1
	MaxNumWorkers     = 64
	DefaultNumWorkers = 8 // TODO: Dynamic worker count discovery will be implemented in a future PR.
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

// Validate checks if the configuration is valid and returns an error if not.
func (c *Config) Validate() error {
	// Use default workers if not specified
	numWorkers := c.NumWorkers
	if numWorkers == 0 {
		numWorkers = DefaultNumWorkers
	}

	return nil
}
