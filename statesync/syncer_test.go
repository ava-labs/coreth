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

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers"
	handlerstats "github.com/ava-labs/coreth/statesync/handlers/stats"
	syncerstats "github.com/ava-labs/coreth/statesync/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const commitCap = 1 * units.MiB
const testMaxRetryDelay = 10 * time.Millisecond

func TestSimpleTrieSync(t *testing.T) {
	var (
		// server stuff
		serverDB     = memorydb.New()
		serverTrieDB = trie.NewDatabase(serverDB)
	)

	root, serverTrie := setupTestTrie(t, serverTrieDB)

	// setup client
	peerID := ids.GenerateTestShortID()
	clientDB := memorydb.New()
	var net peer.Network
	sender := newTestSender(t, 10*time.Millisecond, func(nodeIDs ids.ShortSet, requestID uint32, requestBytes []byte) error {
		assert.Len(t, nodeIDs, 1)
		nodeID, exists := nodeIDs.Pop()
		assert.True(t, exists)
		assert.Equal(t, nodeID, peerID)
		go func() {
			err := net.AppRequest(nodeID, requestID, defaultDeadline(), requestBytes)
			assert.NoError(t, err)
		}()
		return nil
	}, func(nodeID ids.ShortID, requestID uint32, bytes []byte) error {
		assert.Equal(t, nodeID, peerID)
		go func() {
			err := net.AppResponse(nodeID, requestID, bytes)
			assert.NoError(t, err)
		}()
		return nil
	})
	codec := getSyncCodec(t)
	net = peer.NewNetwork(sender, codec, ids.ShortEmpty, 4)
	sender.network = net
	netClient := peer.NewClient(net)

	assert.NoError(t, net.Connected(peerID, StateSyncVersion))
	client := NewClient(netClient, 16, testMaxRetryDelay, codec, nil)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
	codeRequestHandler := handlers.NewCodeRequestHandler(nil, handlerstats.NewNoopHandlerStats(), codec)
	syncHandler := handlers.NewSyncHandler(leafsRequestHandler, nil, nil, codeRequestHandler)
	net.SetRequestHandler(syncHandler)
	s := NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, nil, client, 4, syncerstats.NewNoOpStats())

	// begin sync
	s.Start(context.Background(), root)
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

func TestAllEvenNumberedRequestIDsDroppedByNetwork(t *testing.T) {
	var (
		// server stuff
		serverDB     = memorydb.New()
		serverTrieDB = trie.NewDatabase(serverDB)
	)

	root, serverTrie := setupTestTrie(t, serverTrieDB)

	// setup client
	peerID := ids.GenerateTestShortID()
	clientDB := memorydb.New()
	var net peer.Network
	sender := newTestSender(t, 10*time.Millisecond, func(set ids.ShortSet, requestID uint32, bytes []byte) error {
		assert.Len(t, set, 1)
		id, exists := set.Pop()
		assert.True(t, exists)
		assert.Equal(t, id, peerID)

		if requestID%2 == 0 {
			// drop it
			return nil
		}

		go func() {
			err := net.AppRequest(id, requestID, defaultDeadline(), bytes)
			assert.NoError(t, err)
		}()
		return nil
	}, func(id ids.ShortID, requestID uint32, bytes []byte) error {
		assert.Equal(t, id, peerID)
		go func() {
			err := net.AppResponse(id, requestID, bytes)
			assert.NoError(t, err)
		}()
		return nil
	})
	codec := getSyncCodec(t)
	net = peer.NewNetwork(sender, codec, ids.ShortEmpty, 4)
	sender.network = net

	netClient := peer.NewClient(net)
	assert.NoError(t, net.Connected(peerID, StateSyncVersion))
	client := NewClient(netClient, 16, testMaxRetryDelay, codec, nil)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), handlerstats.NewNoopHandlerStats(), codec)
	syncHandler := handlers.NewSyncHandler(leafsRequestHandler, nil, nil, codeRequestHandler)
	net.SetRequestHandler(syncHandler)
	s := NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, nil, client, 4, syncerstats.NewNoOpStats())

	// begin sync
	s.Start(context.Background(), root)
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

var _ message.RequestHandler = &testRequestHandlerWrapper{}

// testRequestHandlerWrapper calls the respective method on underlying syncRequestHandler
// if the corresponding function (onLeafsRequestFn, etc) returns (nil, nil)
type testRequestHandlerWrapper struct {
	syncRequestHandler message.RequestHandler
	onLeafsRequestFn   func(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error)
	onBlockRequestFn   func(ctx context.Context, nodeID ids.ShortID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error)
	onCodeRequestFn    func(ctx context.Context, nodeID ids.ShortID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error)
}

func (t *testRequestHandlerWrapper) HandleStateTrieLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	if t.onLeafsRequestFn != nil {
		responseBytes, err := t.onLeafsRequestFn(ctx, nodeID, requestID, leafsRequest)
		if responseBytes != nil || err != nil {
			return responseBytes, err
		}
	}

	return t.syncRequestHandler.HandleStateTrieLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (t *testRequestHandlerWrapper) HandleAtomicTrieLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	if t.onLeafsRequestFn != nil {
		responseBytes, err := t.onLeafsRequestFn(ctx, nodeID, requestID, leafsRequest)
		if responseBytes != nil || err != nil {
			return responseBytes, err
		}
	}

	return t.syncRequestHandler.HandleAtomicTrieLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (t *testRequestHandlerWrapper) HandleBlockRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	if t.onBlockRequestFn != nil {
		responseBytes, err := t.onBlockRequestFn(ctx, nodeID, requestID, blockRequest)
		if responseBytes != nil || err != nil {
			return responseBytes, err
		}
	}

	return t.syncRequestHandler.HandleBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (t *testRequestHandlerWrapper) HandleCodeRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	if t.onCodeRequestFn != nil {
		responseBytes, err := t.onCodeRequestFn(ctx, nodeID, requestID, codeRequest)
		if responseBytes != nil || err != nil {
			return responseBytes, err
		}
	}

	return t.syncRequestHandler.HandleCodeRequest(ctx, nodeID, requestID, codeRequest)
}

func TestAllEvenNumberedRequestIDsMishandledByPeer(t *testing.T) {
	var (
		// server stuff
		serverDB     = memorydb.New()
		serverTrieDB = trie.NewDatabase(serverDB)
	)

	root, serverTrie := setupTestTrie(t, serverTrieDB)

	// setup client
	peerID := ids.GenerateTestShortID()
	clientDB := memorydb.New()
	var net peer.Network
	sender := newTestSender(t, 10*time.Millisecond, func(set ids.ShortSet, requestID uint32, bytes []byte) error {
		assert.Len(t, set, 1)
		id, exists := set.Pop()
		assert.True(t, exists)
		assert.Equal(t, id, peerID)
		go func() {
			err := net.AppRequest(id, requestID, defaultDeadline(), bytes)
			assert.NoError(t, err)
		}()
		return nil
	}, func(id ids.ShortID, requestID uint32, bytes []byte) error {
		assert.Equal(t, id, peerID)
		go func() {
			err := net.AppResponse(id, requestID, bytes)
			assert.NoError(t, err)
		}()
		return nil
	})
	codec := getSyncCodec(t)
	net = peer.NewNetwork(sender, codec, ids.ShortEmpty, 4)
	sender.network = net

	netClient := peer.NewClient(net)
	assert.NoError(t, net.Connected(peerID, StateSyncVersion))
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), handlerstats.NewNoopHandlerStats(), codec)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
	syncHandler := handlers.NewSyncHandler(leafsRequestHandler, nil, nil, codeRequestHandler)
	testHandlerWrapper := &testRequestHandlerWrapper{
		syncRequestHandler: syncHandler,
		onLeafsRequestFn: func(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
			if requestID%2 != 0 {
				// if its not even, process as normal
				return nil, nil
			}

			return codec.Marshal(message.Version, &message.LeafsResponse{Keys: [][]byte{[]byte("somekey")}, Vals: [][]byte{[]byte("someval")}})
		},
		onCodeRequestFn: func(ctx context.Context, nodeID ids.ShortID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
			if requestID%2 != 0 {
				// if its not even, process as normal
				return nil, nil
			}

			return codec.Marshal(message.Version, &message.CodeResponse{Data: []byte("some code that is totally invalid")})
		},
	}
	net.SetRequestHandler(testHandlerWrapper)

	client := NewClient(netClient, 16, testMaxRetryDelay, codec, nil)
	s := NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, nil, client, 8, syncerstats.NewNoOpStats())

	// begin sync
	s.Start(context.Background(), root)
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
	leafs  *message.LeafsResponse
	blocks []*types.Block
	code   []byte
	err    error
	waitCh <-chan struct{}
}

var (
	_            Client = &testingClient{}
	errErrClient        = errors.New("always returns this error")
)

func (tc *testingClient) GetLeafs(req message.LeafsRequest) (*message.LeafsResponse, error) {
	if tc.waitCh != nil {
		<-tc.waitCh
	}
	return tc.leafs, tc.err
}

func (tc *testingClient) GetBlocks(common.Hash, uint64, uint16) ([]*types.Block, error) {
	if tc.waitCh != nil {
		<-tc.waitCh
	}
	return tc.blocks, tc.err
}

func (tc *testingClient) GetCode(common.Hash) ([]byte, error) {
	if tc.waitCh != nil {
		<-tc.waitCh
	}
	return tc.code, tc.err
}

func TestErrorsPropagateFromGoroutines(t *testing.T) {
	clientDB := memorydb.New()
	defer clientDB.Close()
	s := NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, nil, &testingClient{err: errErrClient}, 2, syncerstats.NewNoOpStats())
	s.Start(context.Background(), common.Hash{})
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
	s := NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, nil, &testingClient{leafs: &message.LeafsResponse{}}, 2, syncerstats.NewNoOpStats())
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx, common.Hash{})
	cancel()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	assert.ErrorIs(t, s.Error(), context.Canceled)
}

type leafTestingClient struct {
	leafs        *message.LeafsResponse
	leafsHandler *handlers.LeafsRequestHandler
	codec        codec.Manager
	limit        uint16
	waitCh       chan struct{}
	ctx          context.Context
	count        uint32
}

var _ Client = &leafTestingClient{}

func (tc *leafTestingClient) GetLeafs(req message.LeafsRequest) (*message.LeafsResponse, error) {
	req.Limit = tc.limit
	nodeID := ids.GenerateTestShortID()
	bytes, err := tc.leafsHandler.OnLeafsRequest(context.Background(), nodeID, 1, req)
	if err != nil {
		return nil, err
	}
	c := NewClient(nil, 0, 0, tc.codec, nil) // only used to parse results
	resp, err := c.parseLeafsResponse(req, bytes)
	if err != nil {
		return nil, err
	}
	leafsResponse := resp.(message.LeafsResponse)
	select {
	case <-tc.waitCh:
		atomic.AddUint32(&tc.count, 1)
		return &leafsResponse, nil
	case <-tc.ctx.Done():
		return nil, errors.New("generic error")
	}
}

func (tc *leafTestingClient) GetBlocks(common.Hash, uint64, uint16) ([]*types.Block, error) {
	panic("unimplemented")
}

func (tc *leafTestingClient) GetCode(common.Hash) ([]byte, error) {
	panic("unimplemented")
}

func TestResumeSync(t *testing.T) {
	codec := getSyncCodec(t)
	serverDB := memorydb.New()
	defer serverDB.Close()

	trieDB := trie.NewDatabase(serverDB)
	root, serverTrie := setupTestTrie(t, trieDB)
	handler := handlers.NewLeafsRequestHandler(trieDB, handlerstats.NewNoopHandlerStats(), codec)

	clientDB := memorydb.New()
	defer clientDB.Close()
	// setup the testingClient with waitCh so it blocks after serving 1 request
	// that gives us time to cancel the context and perform a resume
	waitCh := make(chan struct{})
	numThreads := 2
	progress, err := NewProgressMarker(clientDB, codec, uint(numThreads))
	if err != nil {
		t.Fatal("could not initialize progress marker", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &leafTestingClient{
		leafsHandler: handler,
		limit:        1,
		codec:        codec,
		waitCh:       waitCh,
		ctx:          ctx,
	}
	s := NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, progress, client, numThreads, syncerstats.NewNoOpStats())
	defer cancel()
	s.Start(ctx, root)
	waitCh <- struct{}{} // one leaf (main trie)

	// Wait for one more leaf to be requested. This guarantees the
	// syncer has processed the response to the 1st leaf and has
	// added the account to the progress marker (asserted below).
	// It is possible the 2nd request is for the main trie or the
	// account trie, and in both cases resume logic should work.
	waitCh <- struct{}{}
	cancel()

	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		assert.Fail(t, "sync not complete in a reasonable time")
		return
	}
	err = s.Error()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error receiving leaves")
	assert.Contains(t, err.Error(), "generic error")

	progress, err = NewProgressMarker(clientDB, codec, uint(numThreads))
	if err != nil {
		t.Fatal("could not initialize progress marker", err)
	}
	accounts, roots := progress.Get()
	assert.Equal(t, 2, len(accounts))
	assert.Equal(t, 2, len(roots))

	// resume
	client.ctx = context.Background()
	s = NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, progress, client, numThreads, syncerstats.NewNoOpStats())
	close(waitCh) // allow work to progress
	s.Start(context.Background(), root)
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
	client.count = 0
	<-snapshot.WipeSnapshot(clientDB, true)
	s = NewLeafSyncer(message.StateTrieNode, 32, clientDB, nil, commitCap, progress, client, numThreads, syncerstats.NewNoOpStats())
	s.Start(context.Background(), newRoot)
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
	assert.Equal(t, uint32(3), client.count)
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
