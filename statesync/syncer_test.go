// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers"
	handlerstats "github.com/ava-labs/coreth/statesync/handlers/stats"
	syncerstats "github.com/ava-labs/coreth/statesync/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const commitCap = 1 * units.MiB

func TestSimpleTrieSync(t *testing.T) {
	var (
		// server stuff
		serverDB     = memorydb.New()
		serverTrieDB = trie.NewDatabase(serverDB)
	)

	root, serverTrie := setupTestTrie(t, serverTrieDB)

	// setup client
	clientDB := memorydb.New()
	codec := getSyncCodec(t)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
	codeRequestHandler := handlers.NewCodeRequestHandler(nil, handlerstats.NewNoopHandlerStats(), codec)
	client := NewMockLeafClient(codec, leafsRequestHandler, codeRequestHandler, nil)

	s, err := NewStateSyncer(root, client, 4, syncerstats.NewNoOpStats(), clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	select {
	case <-s.Done():
		// expect sync to be done in a reasonable time.
	case <-time.After(1 * time.Minute):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	assert.NoError(t, s.Error())

	// get the two tries and ensure they have equal nodes
	clientTrieDB := trie.NewDatabase(clientDB)
	clientTrie, err := trie.New(root, clientTrieDB)
	assert.NoError(t, err, "client trie must initialise with synced root")

	// ensure trie hashes are the same
	assert.Equal(t, serverTrie.Hash(), clientTrie.Hash(), "server trie hash and client trie hash must match")

	clientIt := trie.NewIterator(clientTrie.NodeIterator(nil))
	clientNodes := 0
	for clientIt.Next() {
		clientNodes++
	}

	serverIt := trie.NewIterator(serverTrie.NodeIterator(nil))
	serverNodes := 0
	for serverIt.Next() {
		serverNodes++
	}
	assert.Equal(t, 3, clientNodes)
	assert.Equal(t, serverNodes, clientNodes)
}

type testingClient struct {
	leafs  message.LeafsResponse
	err    error
	waitCh <-chan struct{}
}

var (
	_            Client = &testingClient{}
	errErrClient        = errors.New("always returns this error")
)

func (tc *testingClient) GetLeafs(req message.LeafsRequest) (message.LeafsResponse, error) {
	if tc.waitCh != nil {
		<-tc.waitCh
	}
	return tc.leafs, tc.err
}

func (tc *testingClient) GetBlocks(common.Hash, uint64, uint16) ([]*types.Block, error) {
	panic("not implemented")
}

func (tc *testingClient) GetCode(common.Hash) ([]byte, error) {
	panic("not implemented")
}

func TestErrorsPropagateFromGoroutines(t *testing.T) {
	clientDB := memorydb.New()
	defer clientDB.Close()
	s, err := NewStateSyncer(common.Hash{}, &testingClient{err: errErrClient}, 2, syncerstats.NewNoOpStats(), clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	assert.ErrorIs(t, s.Error(), errErrClient)
}

func TestCancel(t *testing.T) {
	clientDB := memorydb.New()
	defer clientDB.Close()
	// setup the testingClient with waitCh so it blocks after serving 1 request
	// that gives us time to cancel the context and assert the correct error
	waitCh := make(chan struct{}, 1)
	waitCh <- struct{}{}
	leafResponse := message.LeafsResponse{
		Keys: [][]byte{[]byte("key")},
		Vals: [][]byte{[]byte("val")},
		More: true, // set more to true so client will attempt more requests
	}
	s, err := NewStateSyncer(common.Hash{}, &testingClient{leafs: leafResponse}, 2, syncerstats.NewNoOpStats(), clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	cancel()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	assert.ErrorIs(t, s.Error(), context.Canceled)
}

func TestResumeSync(t *testing.T) {
	codec := getSyncCodec(t)
	serverDB := memorydb.New()
	defer serverDB.Close()

	trieDB := trie.NewDatabase(serverDB)
	root, serverTrie := setupTestTrie(t, trieDB)
	leafsHandler := handlers.NewLeafsRequestHandler(trieDB, handlerstats.NewNoopHandlerStats(), codec)

	clientDB := memorydb.New()
	defer clientDB.Close()
	// setup the testingClient with waitCh so it blocks after serving 1 request
	// that gives us time to cancel the context and perform a resume
	waitCh := make(chan struct{})
	numThreads := 2
	ctx, cancel := context.WithCancel(context.Background())
	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	count := uint32(0)
	client := NewMockLeafClient(codec, leafsHandler, nil, nil)
	client.GetLeafsIntercept = func(response message.LeafsResponse) (message.LeafsResponse, error) {
		limit := 1
		if len(response.Keys) > limit {
			response.Keys = response.Keys[:limit]
			response.Vals = response.Vals[:limit]
			response.More = true
		}
		select {
		case <-waitCh:
			atomic.AddUint32(&count, 1)
			return response, nil
		case <-clientCtx.Done():
			return message.LeafsResponse{}, errors.New("generic error")
		}
	}
	s, err := NewStateSyncer(root, client, numThreads, syncerstats.NewNoOpStats(), clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(ctx)
	waitCh <- struct{}{} // one leaf (main trie)

	// Wait for one more leaf to be requested. This guarantees the
	// syncer has processed the response to the 1st leaf and has
	// added the account to the progress marker (asserted below).
	// It is possible the 2nd request is for the main trie or the
	// account trie, and in both cases resume logic should work.
	waitCh <- struct{}{}
	cancel()
	close(waitCh) // allow work to progress

	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	assert.ErrorIs(t, s.Error(), context.Canceled)

	// resume
	s, err = NewStateSyncer(root, client, numThreads, syncerstats.NewNoOpStats(), clientDB, s.commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// assert number of in progress storage tries
	assert.Len(t, s.progressMarker.StorageTries, 1)

	s.Start(context.Background())
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	if err := s.Error(); err != nil {
		t.Fatal("error in completing resumed sync", err)
	}

	// ensure all data is copied
	it := serverDB.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		key := it.Key()
		if len(key) != common.HashLength {
			continue
		}
		val, err := clientDB.Get(it.Key())
		assert.NoError(t, err)
		assert.True(t, bytes.Equal(val, it.Value()))
	}

	// update acc-2 balance and commit trie
	acc := &types.StateAccount{Balance: big.NewInt(1000), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash[:]}
	val, err := rlp.EncodeToBytes(acc)
	assert.NoError(t, err)
	assert.NoError(t, serverTrie.TryUpdate([]byte("acc-2"), val))
	newRoot, _, err := serverTrie.Commit(nil)
	assert.NoError(t, err)
	assert.NoError(t, trieDB.Commit(newRoot, false, nil))

	// reset count and do another sync
	count = 0
	<-snapshot.WipeSnapshot(clientDB, true)
	s, err = NewStateSyncer(newRoot, client, numThreads, syncerstats.NewNoOpStats(), clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	if err := s.Error(); err != nil {
		t.Fatal("error in completing updated trie sync", err)
	}

	// should only request leaves in main trie
	// storage trie will be on disk already
	assert.Equal(t, uint32(3), count)
}

// setupTestTrie creates a trie in [triedb]
// returns root hash, trie
func setupTestTrie(t *testing.T, triedb *trie.Database) (common.Hash, *trie.SecureTrie) {
	// cargo culting from generate_test.go
	stTrie, _ := trie.NewSecure(common.Hash{}, triedb)
	stTrie.Update([]byte("key-1"), []byte("val-1")) // 0x1314700b81afc49f94db3623ef1df38f3ed18b73a1b7ea2f6c095118cf6118a0
	stTrie.Update([]byte("key-2"), []byte("val-2")) // 0x18a0f4d79cff4459642dd7604f303886ad9d77c30cf3d7d7cedb3a693ab6d371
	stTrie.Update([]byte("key-3"), []byte("val-3")) // 0x51c71a47af0695957647fb68766d0becee77e953df17c29b3c2f25436f055c78
	stTrie.Commit(nil)                              // Root: 0xddefcd9376dd029653ef384bd2f0a126bb755fe84fdcc9e7cf421ba454f2bc67

	accTrie, err := trie.NewSecure(common.Hash{}, triedb)
	assert.NoError(t, err)
	acc := &types.StateAccount{Balance: big.NewInt(1), Root: stTrie.Hash(), CodeHash: types.EmptyCodeHash[:]}
	val, err := rlp.EncodeToBytes(acc)
	assert.NoError(t, err)
	accTrie.Update([]byte("acc-1"), val)

	acc = &types.StateAccount{Balance: big.NewInt(2), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash[:]}
	val, err = rlp.EncodeToBytes(acc)
	assert.NoError(t, err)
	accTrie.Update([]byte("acc-2"), val)

	acc = &types.StateAccount{Balance: big.NewInt(3), Root: stTrie.Hash(), CodeHash: types.EmptyCodeHash[:]}
	val, err = rlp.EncodeToBytes(acc)
	assert.NoError(t, err)
	accTrie.Update([]byte("acc-3"), val)
	root, _, err := accTrie.Commit(nil) // Root: 0xa819054cfef894169a5b56ccc4e5e06f14829d4a57498e8b9fb13ff21491828d
	assert.NoError(t, err)
	err = triedb.Commit(root, false, nil)
	assert.NoError(t, err)
	err = triedb.Commit(stTrie.Hash(), false, nil)
	assert.NoError(t, err)

	return root, accTrie
}
