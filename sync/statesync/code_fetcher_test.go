// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
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

	// Init first; AddCode should enqueue a single instance even with duplicates.
	codeBytes := utils.RandomBytes(32)
	codeHash := crypto.Keccak256Hash(codeBytes)

	// Init and enqueue once despite duplicate input.
	require.NoError(t, fetcher.Init())
	require.NoError(t, fetcher.AddCode([]common.Hash{codeHash, codeHash}))

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

func TestCodeFetcherQueue_Init_ResumeFromDB(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Persist a to-fetch marker prior to construction.
	codeBytes := utils.RandomBytes(20)
	want := crypto.Keccak256Hash(codeBytes)
	customrawdb.AddCodeToFetch(db, want)

	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}), WithAutoInit(false))
	require.NoError(t, err)

	// Before Init, nothing should be readable.
	select {
	case <-fetcher.CodeHashes():
		t.Fatal("hash should not be available before Init")
	default:
	}

	// Init should surface the pre-seeded DB marker.
	require.NoError(t, fetcher.Init())

	got := <-fetcher.CodeHashes()
	require.Equal(t, want, got)

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

func TestCodeFetcherQueue_Init_AddCodeBlocks(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Prepare a hash that will be added via AddCode (which should block pre-Init).
	codeBytes := utils.RandomBytes(10)
	want := crypto.Keccak256Hash(codeBytes)

	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}), WithAutoInit(false))
	require.NoError(t, err)

	// Before Init, nothing should be readable.
	select {
	case <-fetcher.CodeHashes():
		t.Fatal("hash should not be available before Init")
	default:
	}

	// Ensure AddCode blocks pre-Init.
	addedCh := make(chan error, 1)
	go func() { addedCh <- fetcher.AddCode([]common.Hash{want}) }()
	select {
	case err := <-addedCh:
		t.Fatalf("AddCode returned before Init: %v", err)
	default:
	}

	// Init unblocks AddCode path.
	require.NoError(t, fetcher.Init())
	require.NoError(t, <-addedCh)

	got := <-fetcher.CodeHashes()
	require.Equal(t, want, got)

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

func TestCodeFetcherQueue_AddCode_Empty(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// No input should enqueue nothing and return nil.
	require.NoError(t, fetcher.AddCode(nil))

	select {
	case <-fetcher.CodeHashes():
		t.Fatal("unexpected hash enqueued")
	default:
	}

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

func TestCodeFetcherQueue_AddCode_PresentCode(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Prepare a sample hash and persist code locally to skip enqueue.
	codeBytes := utils.RandomBytes(33)
	h := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(db, h, codeBytes)

	require.NoError(t, fetcher.AddCode([]common.Hash{h}))

	select {
	case <-fetcher.CodeHashes():
		t.Fatal("unexpected hash enqueued")
	default:
	}

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

func TestCodeFetcherQueue_AddCode_Duplicates(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Prepare a sample hash and submit duplicates; expect single enqueue.
	codeBytes := utils.RandomBytes(33)
	h := crypto.Keccak256Hash(codeBytes)

	require.NoError(t, fetcher.AddCode([]common.Hash{h, h}))

	got := <-fetcher.CodeHashes()
	require.Equal(t, h, got)

	select {
	case <-fetcher.CodeHashes():
		t.Fatal("unexpected extra hash enqueued")
	default:
	}

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

// Test queuing several distinct code hashes at once preserves order and enqueues all.
func TestCodeFetcherQueue_AddCode_MultipleHashes(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Prepare several distinct code hashes.
	num := 10
	inputs := make([]common.Hash, 0, num)
	for i := 0; i < num; i++ {
		h := crypto.Keccak256Hash([]byte(fmt.Sprintf("code-%d", i)))
		inputs = append(inputs, h)
	}

	require.NoError(t, fetcher.AddCode(inputs))

	// Drain exactly num items and assert order is preserved.
	results := make([]common.Hash, 0, num)
	for i := 0; i < num; i++ {
		results = append(results, <-fetcher.CodeHashes())
	}
	require.Equal(t, inputs, results)

	// Ensure no unexpected extra value.
	select {
	case <-fetcher.CodeHashes():
		t.Fatal("unexpected hash enqueued")
	default:
	}

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

// Test that Finalize closes the channel; and no further sends happen.
func TestCodeFetcherQueue_FinalizeCloses(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)

	// After finalize, AddCode with empty input should return an error.
	err = fetcher.AddCode(nil)
	require.ErrorIs(t, err, errFailedToAddCodeHashesToQueue)

	// After finalize, AddCode with non-empty input should return an error and enqueue nothing.
	codeBytes := utils.RandomBytes(8)
	h := crypto.Keccak256Hash(codeBytes)
	err = fetcher.AddCode([]common.Hash{h})
	require.ErrorIs(t, err, errFailedToAddCodeHashesToQueue)
}

// Test that shutdown during enqueue returns the expected error.
func TestCodeFetcherQueue_ShutdownDuringEnqueue(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Create a done channel we control and a very small buffer to force blocking.
	done := make(chan struct{})
	fetcher, err := NewCodeFetcherQueue(db, done, WithMaxOutstandingCodeHashes(1))
	require.NoError(t, err)

	// Fill the buffer with one hash so the next enqueue would block if not for done.
	codeBytes1 := utils.RandomBytes(16)
	codeHash1 := crypto.Keccak256Hash(codeBytes1)
	require.NoError(t, fetcher.AddCode([]common.Hash{codeHash1}))

	// Now close done to simulate shutdown while trying to enqueue another hash.
	close(done)

	codeBytes2 := utils.RandomBytes(16)
	codeHash2 := crypto.Keccak256Hash(codeBytes2)
	err = fetcher.AddCode([]common.Hash{codeHash2})
	require.ErrorIs(t, err, errFailedToAddCodeHashesToQueue)
}
