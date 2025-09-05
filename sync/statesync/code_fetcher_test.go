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

	result := <-fetcher.CodeHashes()
	require.Equal(t, want, result)

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

	// Ensure AddCode blocks pre-Init (synchronize start to avoid race).
	addedCh := make(chan error, 1)
	called := make(chan struct{})
	go func() {
		close(called)
		addedCh <- fetcher.AddCode([]common.Hash{want})
	}()
	<-called

	// Verify AddCode is blocked.
	select {
	case err := <-addedCh:
		t.Fatalf("AddCode returned before Init: %v", err)
	default:
	}

	// Init unblocks AddCode path.
	require.NoError(t, fetcher.Init())
	require.NoError(t, <-addedCh)

	result := <-fetcher.CodeHashes()
	require.Equal(t, want, result)

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

	// Prepare a sample hash and submit duplicates - expect single enqueue.
	codeBytes := utils.RandomBytes(33)
	h := crypto.Keccak256Hash(codeBytes)

	require.NoError(t, fetcher.AddCode([]common.Hash{h, h}))

	result := <-fetcher.CodeHashes()
	require.Equal(t, h, result)

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

// Test that Finalize closes the channel and no further sends happen.
func TestCodeFetcherQueue_FinalizeCloses(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	fetcher.Finalize()
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)

	// After finalize, AddCode with empty input should return an error.
	err = fetcher.AddCode(nil)
	require.ErrorIs(t, err, errAddCodeAfterFinalize)

	// After finalize, AddCode with non-empty input should return an error and enqueue nothing.
	codeBytes := utils.RandomBytes(8)
	h := crypto.Keccak256Hash(codeBytes)
	err = fetcher.AddCode([]common.Hash{h})
	require.ErrorIs(t, err, errAddCodeAfterFinalize)
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

// Test that Finalize waits for in-flight AddCode calls to complete before closing the channel.
func TestCodeFetcherQueue_FinalizeWaitsForInflightAddCodeCalls(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	fetcher, err := NewCodeFetcherQueue(db, make(chan struct{}), WithMaxOutstandingCodeHashes(1))
	require.NoError(t, err)

	numHashes := 3

	// Prepare multiple distinct hashes to exceed the buffer and cause AddCode to block on enqueue.
	hashes := make([]common.Hash, 0, numHashes)
	for i := 0; i < numHashes; i++ {
		hashes = append(hashes, crypto.Keccak256Hash([]byte(fmt.Sprintf("code-%d", i))))
	}

	addDone := make(chan error, 1)
	go func() {
		addDone <- fetcher.AddCode(hashes)
	}()

	// Read the first enqueued hash to ensure AddCode is actively enqueuing and will block on the next send.
	first := <-fetcher.CodeHashes()

	// Call Finalize concurrently - it should block until AddCode returns.
	finalized := make(chan struct{}, 1)
	go func() {
		fetcher.Finalize()
		close(finalized)
	}()

	// Finalize should not complete yet because AddCode is still enqueuing (buffer=1 and we haven't drained).
	select {
	case <-finalized:
		t.Fatal("Finalize returned before in-flight AddCode completed")
	default:
	}

	// Drain remaining enqueued hashes; this will unblock AddCode so it can finish.
	result := make([]common.Hash, 0, numHashes)
	result = append(result, first)
	for i := 1; i < len(hashes); i++ {
		result = append(result, <-fetcher.CodeHashes())
	}
	require.Equal(t, hashes, result)

	// Now AddCode should complete without error, and Finalize should return and close the channel.
	require.NoError(t, <-addDone)

	// Wait for finalize to complete and close the channel.
	<-finalized
	_, ok := <-fetcher.CodeHashes()
	require.False(t, ok)
}

// Test that if hashes are added and the process shuts down abruptly, the
// "to-fetch" markers remain in the DB and are recovered on restart.
func TestCodeFetcherQueue_PersistsMarkersAcrossRestart(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// First run: add a handful of hashes and do not finalize or consume.
	fetcher1, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	num := 5
	inputs := make([]common.Hash, 0, num)
	for i := 0; i < num; i++ {
		inputs = append(inputs, crypto.Keccak256Hash([]byte(fmt.Sprintf("persist-%d", i))))
	}
	require.NoError(t, fetcher1.AddCode(inputs))

	// Assert markers exist in the DB.
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	seen := make(map[common.Hash]struct{}, num)
	for it.Next() {
		h := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		seen[h] = struct{}{}
	}
	require.NoError(t, it.Error())

	for _, h := range inputs {
		if _, ok := seen[h]; !ok {
			t.Fatalf("missing marker for hash %v", h)
		}
	}

	// Restart: construct a new fetcher over the same DB. It should recover
	// the outstanding markers and enqueue them on Init.
	//
	// NOTE: This actually simulates a restart at the component level by discarding the first
	// CodeFetcherQueue (which holds all in-memory state: channels, outstanding, etc.)
	// and constructing a new one over the same DB handle. Given that the DB is the single source
	// of truth for the "to-fetch" markers, the new fetcher immediately recovers the markers and
	// enqueues them on Init.
	fetcher2, err := NewCodeFetcherQueue(db, make(chan struct{}))
	require.NoError(t, err)

	recovered := make(map[common.Hash]struct{}, num)
	for i := 0; i < num; i++ {
		recovered[<-fetcher2.CodeHashes()] = struct{}{}
	}

	for _, h := range inputs {
		if _, ok := recovered[h]; !ok {
			t.Fatalf("missing recovered hash %v", h)
		}
	}

	fetcher2.Finalize()
	_, ok := <-fetcher2.CodeHashes()
	require.False(t, ok)
}
