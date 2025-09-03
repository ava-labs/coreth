// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
)

// Test that adding the same hash multiple times only enqueues once.
func TestCodeFetcherQueue_DedupesEnqueues(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}), WithAutoInit(false))
	require.NoError(t, err)

	// Don't Init yet; AddCode should block.
	codeBytes := utils.RandomBytes(32)
	codeHash := crypto.Keccak256Hash(codeBytes)

	added := make(chan error, 1)
	go func() { added <- fetcher.AddCode([]common.Hash{codeHash, codeHash}) }()

	// Init unblocks AddCode and should enqueue once.
	require.NoError(t, fetcher.Init())
	require.NoError(t, <-added)

	// Should receive exactly one hash, then block until finalize.
	got := <-fetcher.CodeHashes()
	require.Equal(t, codeHash, got)

	// No second copy should be present immediately.
	select {
	case <-fetcher.CodeHashes():
		t.Fatal("unexpected duplicate hash enqueued")
	default:
	}

	fetcher.Finalize()
	// Channel should close without more values.
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

func TestCodeFetcherQueue_Init(t *testing.T) {
	testCases := map[string]struct {
		seedDB bool
	}{
		"resume from db":  {seedDB: true},
		"add code blocks": {seedDB: false},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()

			var want common.Hash
			if tc.seedDB {
				// Persist a to-fetch marker prior to construction.
				codeBytes := utils.RandomBytes(20)
				want = crypto.Keccak256Hash(codeBytes)
				customrawdb.AddCodeToFetch(db, want)
			} else {
				// Prepare a hash that will be added via AddCode (which blocks pre-Init).
				codeBytes := utils.RandomBytes(10)
				want = crypto.Keccak256Hash(codeBytes)
			}

			fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}), WithAutoInit(false))
			require.NoError(t, err)

			// Before Init, nothing should be readable.
			select {
			case <-fetcher.CodeHashes():
				t.Fatal("hash should not be available before Init")
			default:
			}

			// If not seeding DB, ensure AddCode blocks pre-Init.
			var addedCh chan error
			if !tc.seedDB {
				addedCh = make(chan error, 1)
				go func() { addedCh <- fetcher.AddCode([]common.Hash{want}) }()
				select {
				case err := <-addedCh:
					t.Fatalf("AddCode returned before Init: %v", err)
				default:
				}
			}

			// Init unblocks either the pre-seeded DB or the AddCode path.
			require.NoError(t, fetcher.Init())
			if addedCh != nil {
				require.NoError(t, <-addedCh)
			}

			got := <-fetcher.CodeHashes()
			require.Equal(t, want, got)

			fetcher.Finalize()
			_, ok := <-fetcher.CodeHashes()
			require.False(t, ok)
		})
	}
}

func TestCodeFetcherQueue_AddCode_AfterInit(t *testing.T) {
	testCases := map[string]struct {
		preWriteToDB bool
		input        func(common.Hash) []common.Hash
		expectQueued int
	}{
		"empty": {
			input:        func(common.Hash) []common.Hash { return nil },
			expectQueued: 0,
		},
		"present code": {
			preWriteToDB: true,
			input:        func(h common.Hash) []common.Hash { return []common.Hash{h} },
			expectQueued: 0,
		},
		"duplicates": {
			preWriteToDB: false,
			input:        func(h common.Hash) []common.Hash { return []common.Hash{h, h} },
			expectQueued: 1,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
			require.NoError(t, err)

			// Prepare a sample hash
			codeBytes := utils.RandomBytes(33)
			h := crypto.Keccak256Hash(codeBytes)
			if tc.preWriteToDB {
				rawdb.WriteCode(db, h, codeBytes)
			}

			require.NoError(t, fetcher.AddCode(tc.input(h)))

			// Drain up to expectQueued items
			for i := 0; i < tc.expectQueued; i++ {
				got := <-fetcher.CodeHashes()
				// In duplicates case, the value should be the sample hash.
				if name == "duplicates" {
					require.Equal(t, h, got)
				}
			}
			// Ensure no unexpected extra value
			select {
			case <-fetcher.CodeHashes():
				t.Fatal("unexpected hash enqueued")
			default:
			}

			fetcher.Finalize()
			_, ok := <-fetcher.CodeHashes()
			require.False(t, ok)
		})
	}
}

// Test that Finalize closes the channel; and no further sends happen.
func TestCodeFetcherQueue_FinalizeCloses(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)

	// After finalize, AddCode should return nil and enqueue nothing.
	require.NoError(t, fetcher.AddCode(nil))
}

// Test that shutdown during enqueue returns the expected error.
func TestCodeFetcherQueue_ShutdownDuringEnqueue(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Use a closed done channel to simulate shutdown.
	done := make(chan struct{})
	close(done)
	fetcher, err := NewCodeFetcherQueue(db, done)
	require.NoError(t, err)

	// Ensure the enqueue send is not ready by making the channel unbuffered
	// without a receiver; this forces the select to pick the done case.
	fetcher.codeHashes = make(chan common.Hash)

	// Ensure not stored locally to go through enqueue path.
	codeBytes := utils.RandomBytes(16)
	codeHash := crypto.Keccak256Hash(codeBytes)

	err = fetcher.AddCode([]common.Hash{codeHash})
	require.ErrorIs(t, err, errFailedToAddCodeHashesToQueue)
}
