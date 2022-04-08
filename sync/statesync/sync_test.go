// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

const testSyncTimeout = 20 * time.Second

type testSyncResult struct {
	root                       common.Hash
	serverTrieDB, clientTrieDB *trie.Database
}

type syncTest struct {
	getContext        func() context.Context
	prepareForTest    func(t *testing.T) (clientDB ethdb.Database, serverTrieDB *trie.Database, syncRoot common.Hash)
	assertSyncResult  func(t *testing.T, result testSyncResult)
	expectedError     error
	GetLeafsIntercept func(message.LeafsRequest, message.LeafsResponse) (message.LeafsResponse, error)
	GetCodeIntercept  func(common.Hash, []byte) ([]byte, error)
}

func testSync(t *testing.T, test syncTest) {
	ctx := context.TODO()
	if test.getContext != nil {
		ctx = test.getContext()
	}
	clientDB, serverTrieDB, root := test.prepareForTest(t)
	codec := message.MustBuildCodec()
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewMockClient(codec, leafsRequestHandler, codeRequestHandler, nil)
	// Set intercept functions for the mock client
	mockClient.GetLeafsIntercept = test.GetLeafsIntercept
	mockClient.GetCodeIntercept = test.GetCodeIntercept

	s, err := NewEVMStateSyncer(&EVMStateSyncerConfig{
		Client:    mockClient,
		Root:      root,
		DB:        clientDB,
		BatchSize: 1000, // Use a lower batch size in order to get test coverage of batches being written early.
	})
	if err != nil {
		t.Fatal(err)
	}
	// begin sync
	s.Start(ctx)
	waitFor(t, s.Done(), test.expectedError, testSyncTimeout)
	if test.expectedError != nil {
		return
	}

	test.assertSyncResult(t, testSyncResult{
		root:         root,
		serverTrieDB: serverTrieDB,
		clientTrieDB: trie.NewDatabase(clientDB),
	})
}

// testSyncResumes tests a series of syncs works as expected invoking a callback function after each
// successive test.
func testSyncResumes(t *testing.T, steps []syncTest, stepCallback func()) {
	for _, test := range steps {
		testSync(t, test)
		stepCallback()
	}
}

// waitFor waits for a result on the [result] channel to match [expected], or a timeout.
func waitFor(t *testing.T, result <-chan error, expected error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-result:
		if expected != nil {
			if err == nil {
				t.Fatalf("Expected error %s, but got nil", expected)
			}
			assert.Contains(t, err.Error(), expected.Error())
		} else if err != nil {
			t.Fatal("unexpected error waiting for sync result", err)
		}
	case <-time.After(timeout):
		t.Fatal("unexpected timeout waiting for sync result")
	}
}

func TestSimpleSyncCases(t *testing.T) {
	clientErr := errors.New("dummy client error")
	tests := map[string]syncTest{
		"accounts": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 1000, nil)
				return memorydb.New(), serverTrieDB, root
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		"accounts with code": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 1000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
					if index%3 == 0 {
						codeBytes := make([]byte, 256)
						_, err := rand.Read(codeBytes)
						if err != nil {
							t.Fatalf("error reading random code bytes: %v", err)
						}

						codeHash := crypto.Keccak256Hash(codeBytes)
						rawdb.WriteCode(serverTrieDB.DiskDB(), codeHash, codeBytes)
						account.CodeHash = codeHash[:]
					}
					return account
				})
				return memorydb.New(), serverTrieDB, root
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		"accounts with code and storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccountsWithStorage(t, serverTrieDB, common.Hash{}, 1000)
				return memorydb.New(), serverTrieDB, root
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		"accounts with storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 1000, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
					if i%5 == 0 {
						account.Root, _, _ = trie.GenerateTrie(t, serverTrieDB, 16, common.HashLength)
					}

					return account
				})
				return memorydb.New(), serverTrieDB, root
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		"accounts with overlapping storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 1000, 3)
				return memorydb.New(), serverTrieDB, root
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		"failed to fetch leafs": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 100, nil)
				return memorydb.New(), serverTrieDB, root
			},
			GetLeafsIntercept: func(_ message.LeafsRequest, _ message.LeafsResponse) (message.LeafsResponse, error) {
				return message.LeafsResponse{}, clientErr
			},
			expectedError: clientErr,
		},
		"failed to fetch code": {
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccountsWithStorage(t, serverTrieDB, common.Hash{}, 100)
				return memorydb.New(), serverTrieDB, root
			},
			GetCodeIntercept: func(_ common.Hash, b []byte) ([]byte, error) {
				return nil, clientErr
			},
			expectedError: clientErr,
		},
	}
	for name, test := range tests {
		rand.Seed(1)
		t.Run(name, func(t *testing.T) {
			testSync(t, test)
		})
	}
}

func TestCancelSync(t *testing.T) {
	serverTrieDB := trie.NewDatabase(memorydb.New())
	// Create trie with 2000 accounts (more than one leaf request)
	root := fillAccountsWithStorage(t, serverTrieDB, common.Hash{}, 2000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testSync(t, syncTest{
		getContext: func() context.Context {
			return ctx
		},
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return memorydb.New(), serverTrieDB, root
		},
		expectedError: context.Canceled,
		GetLeafsIntercept: func(_ message.LeafsRequest, lr message.LeafsResponse) (message.LeafsResponse, error) {
			cancel()
			return lr, nil
		},
	})
}

func TestResumeSyncAccountsTrieInterrupted(t *testing.T) {
	serverTrieDB := trie.NewDatabase(memorydb.New())
	root, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 2000, 3)
	errInterrupted := errors.New("interrupted sync")
	clientDB := memorydb.New()
	accountLeavesRequests := 0
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		expectedError: errInterrupted,
		GetLeafsIntercept: func(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
			if request.Root == root && accountLeavesRequests >= 1 {
				return message.LeafsResponse{}, errInterrupted
			}
			if request.Root == root {
				accountLeavesRequests++
			}
			return response, nil
		},
	})

	assert.Equal(t, 1, accountLeavesRequests)

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		assertSyncResult: func(t *testing.T, result testSyncResult) {
			assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
		},
	})
}

func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	serverTrieDB := trie.NewDatabase(memorydb.New())

	largeStorageRoot, _, _ := trie.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot
		}
		return account
	})
	errInterrupted := errors.New("interrupted sync")
	clientDB := memorydb.New()
	largeStorageRootRequests := 0
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		expectedError: errInterrupted,
		GetLeafsIntercept: func(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
			if request.Root == largeStorageRoot && largeStorageRootRequests >= 1 {
				return message.LeafsResponse{}, errInterrupted
			}
			if request.Root == largeStorageRoot {
				largeStorageRootRequests++
			}
			return response, nil
		},
	})

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		assertSyncResult: func(t *testing.T, result testSyncResult) {
			assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
		},
	})
}

func TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted(t *testing.T) {
	serverTrieDB := trie.NewDatabase(memorydb.New())

	largeStorageRoot1, _, _ := trie.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	largeStorageRoot2, _, _ := trie.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root1, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot1
		}
		return account
	})
	root2, _ := FillAccounts(t, serverTrieDB, root1, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		if index == 20 {
			account.Root = largeStorageRoot2
		}
		return account
	})
	errInterrupted := errors.New("interrupted sync")
	clientDB := memorydb.New()
	largeStorageRootRequests := 0
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root1
		},
		expectedError: errInterrupted,
		GetLeafsIntercept: func(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
			if request.Root == largeStorageRoot1 && largeStorageRootRequests >= 1 {
				return message.LeafsResponse{}, errInterrupted
			}
			if request.Root == largeStorageRoot1 {
				largeStorageRootRequests++
			}
			return response, nil
		},
	})

	<-snapshot.WipeSnapshot(clientDB, false)

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root2
		},
		assertSyncResult: func(t *testing.T, result testSyncResult) {
			assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
		},
	})
}

func TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted(t *testing.T) {
	serverTrieDB := trie.NewDatabase(memorydb.New())

	largeStorageRoot, _, _ := trie.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for 2 successive accounts
		if index == 10 || index == 11 {
			account.Root = largeStorageRoot
		}
		return account
	})
	errInterrupted := errors.New("interrupted sync")
	clientDB := memorydb.New()
	largeStorageRootRequests := 0
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		expectedError: errInterrupted,
		GetLeafsIntercept: func(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
			if request.Root == largeStorageRoot && largeStorageRootRequests >= 1 {
				return message.LeafsResponse{}, errInterrupted
			}
			if request.Root == largeStorageRoot {
				largeStorageRootRequests++
			}
			return response, nil
		},
	})

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		assertSyncResult: func(t *testing.T, result testSyncResult) {
			assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
		},
	})
}

func TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted(t *testing.T) {
	serverTrieDB := trie.NewDatabase(memorydb.New())

	largeStorageRoot, _, _ := trie.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		if index == 10 || index == 90 {
			account.Root = largeStorageRoot
		}
		return account
	})
	errInterrupted := errors.New("interrupted sync")
	clientDB := memorydb.New()
	largeStorageRootRequests := 0
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		expectedError: errInterrupted,
		GetLeafsIntercept: func(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
			if request.Root == largeStorageRoot && largeStorageRootRequests >= 1 {
				return message.LeafsResponse{}, errInterrupted
			}
			if request.Root == largeStorageRoot {
				largeStorageRootRequests++
			}
			return response, nil
		},
	})

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
			return clientDB, serverTrieDB, root
		},
		assertSyncResult: func(t *testing.T, result testSyncResult) {
			assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
		},
	})
}

func TestResyncNewRootAfterDeletes(t *testing.T) {
	for name, test := range map[string]struct {
		deleteBetweenSyncs func(*testing.T, common.Hash, *trie.Database)
	}{
		"delete code": {
			deleteBetweenSyncs: func(t *testing.T, _ common.Hash, clientTrieDB *trie.Database) {
				db := clientTrieDB.DiskDB()
				// delete code
				it := db.NewIterator(rawdb.CodePrefix, nil)
				defer it.Release()
				for it.Next() {
					if len(it.Key()) != len(rawdb.CodePrefix)+common.HashLength {
						continue
					}
					if err := db.Delete(it.Key()); err != nil {
						t.Fatal(err)
					}
				}
				if err := it.Error(); err != nil {
					t.Fatal(err)
				}
			},
		},
		"delete intermediate storage nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientTrieDB *trie.Database) {
				tr, err := trie.New(root, clientTrieDB)
				if err != nil {
					t.Fatal(err)
				}
				it := trie.NewIterator(tr.NodeIterator(nil))
				accountsWithStorage := 0

				// keep track of storage tries we delete trie nodes from
				// so we don't try to do it again if another account has
				// the same storage root.
				corruptedStorageRoots := make(map[common.Hash]struct{})
				for it.Next() {
					var acc types.StateAccount
					if err := rlp.DecodeBytes(it.Value, &acc); err != nil {
						t.Fatal(err)
					}
					if acc.Root == types.EmptyRootHash {
						continue
					}
					if _, found := corruptedStorageRoots[acc.Root]; found {
						// avoid trying to delete nodes from a trie we have already deleted nodes from
						continue
					}
					accountsWithStorage++
					if accountsWithStorage%2 != 0 {
						continue
					}
					corruptedStorageRoots[acc.Root] = struct{}{}
					trie.CorruptTrie(t, clientTrieDB, acc.Root, 2)
				}
				if err := it.Err; err != nil {
					t.Fatal(err)
				}
			},
		},
		"delete intermediate account trie nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientTrieDB *trie.Database) {
				trie.CorruptTrie(t, clientTrieDB, root, 5)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			testSyncerSyncsToNewRoot(t, test.deleteBetweenSyncs)
		})
	}
}

func testSyncerSyncsToNewRoot(t *testing.T, deleteBetweenSyncs func(*testing.T, common.Hash, *trie.Database)) {
	rand.Seed(1)
	clientDB := memorydb.New()
	serverTrieDB := trie.NewDatabase(memorydb.New())

	root1, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 1000, 3)
	root2, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, root1, 1000, 3)

	called := false

	testSyncResumes(t, []syncTest{
		{
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				return clientDB, serverTrieDB, root1
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, root1, serverTrieDB, trie.NewDatabase(clientDB))
			},
		},
		{
			prepareForTest: func(t *testing.T) (ethdb.Database, *trie.Database, common.Hash) {
				return clientDB, serverTrieDB, root2
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, root2, serverTrieDB, trie.NewDatabase(clientDB))
			},
		},
	}, func() {
		// Only perform the delete stage once
		if called {
			return
		}
		called = true
		// delete snapshot first since this is not the responsibility of the EVM State Syncer
		<-snapshot.WipeSnapshot(clientDB, false)

		deleteBetweenSyncs(t, root1, trie.NewDatabase(clientDB))
	})
}
