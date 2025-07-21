// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/coreth/plugin/evm/atomic/state"
	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/sync/statesync/statesynctest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
)

const commitInterval = 1024

type atomicSyncTestCheckpoint struct {
	expectedNumLeavesSynced int64       // expected number of leaves to have synced at this checkpoint
	leafCutoff              int         // Number of leafs to sync before cutting off responses
	targetRoot              common.Hash // Root of trie to resume syncing from after stopping
	targetHeight            uint64      // Height to sync to after stopping
}

// testSyncer creates a leaf handler with [serverTrieDB] and tests to ensure that the atomic syncer can sync correctly
// starting at [targetRoot], and stopping and resuming at each of the [checkpoints].
func testSyncer(t *testing.T, serverTrieDB *triedb.Database, targetHeight uint64, targetRoot common.Hash, checkpoints []atomicSyncTestCheckpoint, finalExpectedNumLeaves int64) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	numLeaves := 0
	mockClient := syncclient.NewTestClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(serverTrieDB, state.TrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	clientDB := versiondb.New(memdb.New())
	repo, err := state.NewAtomicTxRepository(clientDB, message.Codec, 0)
	require.NoError(t, err, "could not initialize atomic tx repository")
	atomicBackend, err := state.NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{}, commitInterval)
	require.NoError(t, err, "could not initialize atomic backend")

	// For each checkpoint, replace the leafsIntercept to shut off the syncer at the correct point and force resume from the checkpoint's
	// next trie.
	for i, checkpoint := range checkpoints {
		// Create syncer targeting the current [syncTrie].
		syncerConfig := Config{
			Client:       mockClient,
			Database:     clientDB,
			AtomicTrie:   atomicBackend.AtomicTrie(),
			TargetRoot:   targetRoot,
			TargetHeight: targetHeight,
			RequestSize:  config.DefaultStateSyncRequestSize,
			NumWorkers:   1,
		}
		syncer, err := newSyncer(&syncerConfig)
		require.NoError(t, err, "could not create syncer")
		mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, leafsResponse message.LeafsResponse) (message.LeafsResponse, error) {
			// If this request exceeds the desired number of leaves, intercept the request with an error
			if numLeaves+len(leafsResponse.Keys) > checkpoint.leafCutoff {
				return message.LeafsResponse{}, fmt.Errorf("intercept cut off responses after %d leaves", checkpoint.leafCutoff)
			}

			// Increment the number of leaves and return the original response
			numLeaves += len(leafsResponse.Keys)
			return leafsResponse, nil
		}

		syncer.Start(ctx)
		err = syncer.Wait(ctx)
		require.Error(t, err, "Expected syncer to fail at checkpoint with numLeaves %d", numLeaves)

		require.Equal(t, checkpoint.expectedNumLeavesSynced, int64(numLeaves), "unexpected number of leaves received at checkpoint %d", i)
		// Replace the target root and height for the next checkpoint
		targetRoot = checkpoint.targetRoot
		targetHeight = checkpoint.targetHeight
	}

	// Create syncer targeting the current [targetRoot].
	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   targetRoot,
		TargetHeight: targetHeight,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   1,
	}
	syncer, err := newSyncer(&syncerConfig)
	require.NoError(t, err, "could not create syncer")
	mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, leafsResponse message.LeafsResponse) (message.LeafsResponse, error) {
		// Increment the number of leaves and return the original response
		numLeaves += len(leafsResponse.Keys)
		return leafsResponse, nil
	}

	syncer.Start(ctx)
	err = syncer.Wait(ctx)
	require.NoError(t, err, "Expected syncer to finish successfully")

	require.Equal(t, finalExpectedNumLeaves, int64(numLeaves), "unexpected number of leaves received to match")

	// we re-initialise trie DB for asserting the trie to make sure any issues with unflushed writes
	// are caught here as this will only pass if all trie nodes have been written to the underlying DB
	atomicTrie := atomicBackend.AtomicTrie()
	clientTrieDB := atomicTrie.TrieDB()
	statesynctest.AssertTrieConsistency(t, targetRoot, serverTrieDB, clientTrieDB, nil)

	// check all commit heights are created correctly
	hasher := trie.NewEmpty(triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil))

	serverTrie, err := trie.New(trie.TrieID(targetRoot), serverTrieDB)
	if err != nil {
		t.Fatalf("failed to create server trie: %v", err)
	}
	addAllKeysWithPrefix := func(prefix []byte) error {
		nodeIt, err := serverTrie.NodeIterator(prefix)
		if err != nil {
			return err
		}
		it := trie.NewIterator(nodeIt)
		for it.Next() {
			if !bytes.HasPrefix(it.Key, prefix) {
				return it.Err
			}
			if err := hasher.Update(it.Key, it.Value); err != nil {
				return err
			}
		}
		return it.Err
	}

	for height := uint64(0); height <= targetHeight; height++ {
		if err := addAllKeysWithPrefix(database.PackUInt64(height)); err != nil {
			t.Fatalf("failed to add keys for height %d: %v", height, err)
		}

		if height%commitInterval == 0 {
			expected := hasher.Hash()
			root, err := atomicTrie.Root(height)
			if err != nil {
				t.Fatalf("failed to get root for height %d: %v", height, err)
			}
			require.Equal(t, expected, root)
		}
	}
}

func TestSyncer(t *testing.T) {
	rand.Seed(1)
	targetHeight := 10 * uint64(commitInterval)
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, int(targetHeight), atomicstate.TrieKeyLength)

	testSyncer(t, serverTrieDB, targetHeight, root, nil, int64(targetHeight))
}

func TestSyncerResume(t *testing.T) {
	rand.Seed(1)
	targetHeight := 10 * uint64(commitInterval)
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	numTrieKeys := int(targetHeight) - 1 // no atomic ops for genesis
	root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, numTrieKeys, atomicstate.TrieKeyLength)

	testSyncer(t, serverTrieDB, targetHeight, root, []atomicSyncTestCheckpoint{
		{
			targetRoot:              root,
			targetHeight:            targetHeight,
			leafCutoff:              commitInterval*5 - 1,
			expectedNumLeavesSynced: commitInterval * 4,
		},
	}, int64(targetHeight)+commitInterval-1) // we will resync the last commitInterval - 1 leafs
}

func TestSyncerResumeNewRootCheckpoint(t *testing.T) {
	rand.Seed(1)
	targetHeight1 := 10 * uint64(commitInterval)
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	numTrieKeys1 := int(targetHeight1) - 1 // no atomic ops for genesis
	root1, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, numTrieKeys1, atomicstate.TrieKeyLength)

	targetHeight2 := 20 * uint64(commitInterval)
	numTrieKeys2 := int(targetHeight2) - 1 // no atomic ops for genesis
	root2, _, _ := statesynctest.FillTrie(
		t, numTrieKeys1, numTrieKeys2, atomicstate.TrieKeyLength, serverTrieDB, root1,
	)

	testSyncer(t, serverTrieDB, targetHeight1, root1, []atomicSyncTestCheckpoint{
		{
			targetRoot:              root2,
			targetHeight:            targetHeight2,
			leafCutoff:              commitInterval*5 - 1,
			expectedNumLeavesSynced: commitInterval * 4,
		},
	}, int64(targetHeight2)+commitInterval-1) // we will resync the last commitInterval - 1 leafs
}

// TestSyncerParallelization verifies that the syncer works correctly with multiple worker goroutines.
func TestSyncerParallelization(t *testing.T) {
	const targetHeight = 2 * uint64(commitInterval) // 2,048 leaves for meaningful parallelization.
	ctx, mockClient, atomicBackend, root := setupParallelizationTest(t, targetHeight)

	runParallelizationTest(t, ctx, mockClient, atomicBackend, root, targetHeight, 4, false)
}

// TestSyncerWaitWithoutStart verifies that Wait() returns an error when called before Start().
func TestSyncerWaitWithoutStart(t *testing.T) {
	_, mockClient, atomicBackend, root := setupParallelizationTest(t, 100)
	clientDB := versiondb.New(memdb.New())

	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   0,
	}
	syncer, err := newSyncer(&syncerConfig)
	require.NoError(t, err, "could not create syncer")

	ctx := context.Background()

	// Wait should return an error when called before Start().
	err = syncer.Wait(ctx)
	require.ErrorIs(t, err, ErrWaitBeforeStart, "should return ErrWaitBeforeStart")
}

// TestSyncerWaitAfterStart verifies that Wait() works correctly after Start() is called.
func TestSyncerWaitAfterStart(t *testing.T) {
	ctx, mockClient, atomicBackend, root := setupParallelizationTest(t, 100)
	clientDB := versiondb.New(memdb.New())

	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   0,
	}
	syncer, err := newSyncer(&syncerConfig)
	require.NoError(t, err, "could not create syncer")

	// Start the syncer.
	err = syncer.Start(ctx)
	require.NoError(t, err, "could not start syncer")

	// Wait should work correctly after Start().
	err = syncer.Wait(ctx)
	require.NoError(t, err, "Wait() should work after Start()")
}

// TestSyncerConstructorValidation verifies that the newSyncer function properly validates numWorkers.
func TestSyncerConstructorValidation(t *testing.T) {
	_, mockClient, atomicBackend, root := setupParallelizationTest(t, 100)
	clientDB := versiondb.New(memdb.New())

	// Test with a valid worker count.
	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   4,
	}
	_, err := newSyncer(&syncerConfig)
	require.NoError(t, err, "should accept valid worker count")

	// Test with too few workers.
	syncerConfig = Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   -1, // Use negative value to trigger validation error.
	}
	_, err = newSyncer(&syncerConfig)
	require.ErrorIs(t, err, ErrTooFewWorkers, "should return ErrTooFewWorkers")

	// Test with too many workers.
	syncerConfig = Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   MaxNumWorkers() + 1,
	}
	_, err = newSyncer(&syncerConfig)
	require.ErrorIs(t, err, ErrTooManyWorkers, "should return ErrTooManyWorkers")
}

// TestSyncerErrorDetails verifies that sentinel errors are wrapped with detailed information.
func TestSyncerErrorDetails(t *testing.T) {
	_, mockClient, atomicBackend, root := setupParallelizationTest(t, 100)
	clientDB := versiondb.New(memdb.New())

	// Test that error details are preserved while sentinel errors are identifiable
	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   -1, // Use negative value to trigger validation error
	}

	_, err := newSyncer(&syncerConfig)
	require.ErrorIs(t, err, ErrTooFewWorkers, "should be identifiable as ErrTooFewWorkers")
	require.Contains(t, err.Error(), "-1 (minimum: 1)", "should contain detailed information")

	syncerConfig = Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   MaxNumWorkers() + 1,
	}
	_, err = newSyncer(&syncerConfig)
	require.ErrorIs(t, err, ErrTooManyWorkers, "should be identifiable as ErrTooManyWorkers")
	require.Contains(t, err.Error(), fmt.Sprintf("%d (maximum: %d)", MaxNumWorkers()+1, MaxNumWorkers()), "should contain detailed information")
}

// TestSyncerDefaultParallelization verifies that the syncer defaults to parallelization.
func TestSyncerDefaultParallelization(t *testing.T) {
	const targetHeight = uint64(commitInterval) // 1,024 leaves to test commit boundary.
	ctx, mockClient, atomicBackend, root := setupParallelizationTest(t, targetHeight)

	runParallelizationTest(t, ctx, mockClient, atomicBackend, root, targetHeight, 0, true)
}

// TestDefaultNumWorkers verifies that the DefaultNumWorkers function returns reasonable values.
func TestDefaultNumWorkers(t *testing.T) {
	workers := DefaultNumWorkers()

	// Should be within bounds
	require.GreaterOrEqual(t, workers, MinNumWorkers, "workers should be greater than or equal to MinNumWorkers")
	require.LessOrEqual(t, workers, MaxNumWorkers(), "workers should be less than or equal to MaxNumWorkers")

	// Should be reasonable relative to CPU count
	cpus := runtime.NumCPU()
	expectedMin := (cpus * 3) / 4 // 75% of CPUs
	if expectedMin > MaxNumWorkers() {
		expectedMin = MaxNumWorkers()
	}
	if expectedMin < MinNumWorkers {
		expectedMin = MinNumWorkers
	}

	// Allow some flexibility due to rounding
	require.GreaterOrEqual(t, workers, expectedMin-1, "workers should be close to 75% of CPU count")
	require.LessOrEqual(t, workers, expectedMin+1, "workers should be close to 75% of CPU count")

	t.Logf("CPU count: %d, Default worker goroutines: %d", cpus, workers)
}

// TestMaxNumWorkers verifies that the MaxNumWorkers function returns reasonable values.
func TestMaxNumWorkers(t *testing.T) {
	maxWorkers := MaxNumWorkers()
	cpus := runtime.NumCPU()

	// Should be at least 2x CPU cores (for I/O bound work).
	expectedMin := cpus * 2
	if expectedMin > 64 {
		expectedMin = 64 // Capped at 64
	}

	require.Equal(t, expectedMin, maxWorkers, "MaxNumWorkers should be 2x CPU cores, capped at 64")

	// Should be reasonable for different CPU counts.
	if cpus <= 32 {
		require.Equal(t, cpus*2, maxWorkers, "For %d CPUs, should allow %d workers", cpus, cpus*2)
	} else {
		require.Equal(t, 64, maxWorkers, "For %d CPUs, should be capped at 64", cpus)
	}

	t.Logf("CPU count: %d, Max worker goroutines: %d", cpus, maxWorkers)
}

// setupParallelizationTest creates the common test infrastructure for parallelization tests.
// It returns the context, mock client, atomic backend, and root hash for testing.
func setupParallelizationTest(t *testing.T, targetHeight uint64) (context.Context, *syncclient.TestClient, *state.AtomicBackend, common.Hash) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Create a simple test trie with some data using the existing pattern.
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, int(targetHeight), atomicstate.TrieKeyLength)

	mockClient := syncclient.NewTestClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(serverTrieDB, state.TrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	clientDB := versiondb.New(memdb.New())
	repo, err := state.NewAtomicTxRepository(clientDB, message.Codec, 0)
	require.NoError(t, err, "could not initialize atomic tx repository")

	atomicBackend, err := state.NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{}, commitInterval)
	require.NoError(t, err, "could not initialize atomic backend")

	return ctx, mockClient, atomicBackend, root
}

// runParallelizationTest executes a parallelization test with the given parameters.
func runParallelizationTest(t *testing.T, ctx context.Context, mockClient *syncclient.TestClient, atomicBackend *state.AtomicBackend, root common.Hash, targetHeight uint64, numWorkers int, useDefaultWorkers bool) {
	clientDB := versiondb.New(memdb.New())

	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: targetHeight,
		RequestSize:  config.DefaultStateSyncRequestSize,
	}

	// Set worker count based on test type
	if useDefaultWorkers {
		syncerConfig.NumWorkers = DefaultNumWorkers()
	} else {
		syncerConfig.NumWorkers = numWorkers
	}

	syncer, err := newSyncer(&syncerConfig)
	require.NoError(t, err, "could not create syncer")

	workerType := "default workers"
	if !useDefaultWorkers {
		workerType = fmt.Sprintf("%d workers", numWorkers)
	}

	err = syncer.Start(ctx)
	require.NoError(t, err, "could not start syncer with %s", workerType)

	// Wait for completion.
	err = syncer.Wait(ctx)
	require.NoError(t, err, "syncer should complete successfully")

	// Verify that the syncer completed successfully.
	select {
	case err := <-syncer.Done():
		require.NoError(t, err, "no error should be returned from Done()")
	default:
		// No error, which is expected
	}
}
