// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

type testSyncResult struct {
	root                       common.Hash
	additionalRoots            []common.Hash
	serverTrieDB, clientTrieDB *trie.Database
}

func TestSyncer(t *testing.T) {
	rand.Seed(1)
	tests := []struct {
		name             string
		prepareForTest   func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) // return trie database and trie root to sync, and any additional roots needed for verification
		assertSyncResult func(t *testing.T, result testSyncResult)
		expectedError    error
	}{
		{
			name: "accounts_only_trie",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, nil)
				return serverTrieDB, root, nil
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		{
			name: "accounts_with_codes_trie",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, func(t *testing.T, index int64, account types.StateAccount, trie *trie.Trie) types.StateAccount {
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
				return serverTrieDB, root, nil
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		{
			name: "missing_sync_root",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				return serverTrieDB, common.BytesToHash([]byte("totally-fake-root")), nil
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "empty_server_trie",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				return trie.NewDatabase(memorydb.New()), common.BytesToHash([]byte("some empty bytes")), nil
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "inconsistent_server_trie",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, nil)

				// delete some random entries from the diskDB
				diskDB := serverTrieDB.DiskDB()
				iter := diskDB.NewIterator(nil, nil)
				defer iter.Release()

				i := 1
				for iter.Next() {
					if rand.Intn(51)%i == 0 {
						if err := diskDB.Delete(iter.Key()); err != nil {
							t.Fatalf("error deleting key, key=%s, err=%s", common.BytesToHash(iter.Key()), err)
						}
					}
					i++
				}
				return serverTrieDB, root, nil
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "sync_non_latest_root",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, nil)
				newRoot := fillAccounts(t, serverTrieDB, root, 500, nil)
				return serverTrieDB, root, []common.Hash{newRoot}
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				// ensure tries are consistent
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)

				clientTrie, err := trie.New(result.root, result.clientTrieDB)
				if err != nil {
					t.Fatalf("error opening client trie, root=%s, err=%v", result.root, err)
				}
				// open server trie at the newer root
				serverTrie, err := trie.New(result.additionalRoots[0], result.serverTrieDB)
				if err != nil {
					t.Fatalf("error opening server trie, root=%s, err=%v", result.additionalRoots[0], err)
				}

				foundHash := make(map[common.Hash]struct{}, 1000)
				clientTrieIter := trie.NewIterator(clientTrie.NodeIterator(nil))
				for clientTrieIter.Next() {
					hash := common.BytesToHash(clientTrieIter.Key)
					foundHash[hash] = struct{}{}
				}

				// look for entries in the serverTrie at the newer root missing in the client trie
				notFound := 0
				serverTrieIter := trie.NewIterator(serverTrie.NodeIterator(nil))
				for serverTrieIter.Next() {
					hash := common.BytesToHash(serverTrieIter.Key)
					if _, exists := foundHash[hash]; !exists {
						notFound++
					}
				}

				// there should be 500 of them
				assert.EqualValues(t, 500, notFound)
			},
		},
		{
			name: "malformed_account",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, nil)

				serverTrie, err := trie.New(root, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening server trie: %v", err)
				}
				// input one malformed account
				accountBytes := []byte("some malformed account is here yo")
				accountHash := crypto.Keccak256Hash(accountBytes)
				if err = serverTrie.TryUpdate(accountHash[:], accountBytes); err != nil {
					t.Fatalf("error updating server trie: %v", err)
				}

				root, _, err = serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}
				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				return serverTrieDB, root, nil
			},
			expectedError: errors.New("rlp: expected input list for types.StateAccount"),
		},
		{
			name: "accounts_with_storage",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccountsWithStorage(t, serverTrieDB, common.Hash{})
				return serverTrieDB, root, nil
			},
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertDBConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB)
			},
		},
		{
			name: "accounts_with_missing_storage",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
					if index%20 == 0 {
						codeBytes := make([]byte, 256)
						_, err := rand.Read(codeBytes)
						if err != nil {
							t.Fatalf("error reading random code bytes: %v", err)
						}

						codeHash := crypto.Keccak256Hash(codeBytes)
						rawdb.WriteCode(serverTrieDB.DiskDB(), codeHash, codeBytes)

						account.CodeHash = codeHash[:]
						account.Root = common.BytesToHash([]byte(fmt.Sprintf("some storage root this is %d", index)))
					}
					return account
				})
				return serverTrieDB, root, nil
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "accounts_with_missing_code",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
					if index%20 == 0 {
						codeBytes := make([]byte, 256)
						_, err := rand.Read(codeBytes)
						if err != nil {
							t.Fatalf("error reading random code bytes: %v", err)
						}

						account.CodeHash = []byte("some code hash which is not a hash at all")
					}
					return account
				})
				return serverTrieDB, root, nil
			},
			expectedError: errors.New("error getting code bytes for code hash"),
		},
		{
			name: "code_hash_mismatch",
			prepareForTest: func(t *testing.T) (*trie.Database, common.Hash, []common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				root := fillAccounts(t, serverTrieDB, common.Hash{}, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
					if index%3 == 0 {
						codeBytes := make([]byte, 256)
						_, err := rand.Read(codeBytes)
						if err != nil {
							t.Fatalf("error reading random code bytes: %v", err)
						}

						codeHash := crypto.Keccak256Hash(codeBytes)
						rawdb.WriteCode(serverTrieDB.DiskDB(), codeHash, codeBytes)

						account.CodeHash = []byte("imma code hash thats not a code hash")
					}
					return account
				})
				return serverTrieDB, root, nil
			},
			expectedError: errors.New("error getting code bytes for code hash"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				clientDB                   *memorydb.Database
				serverTrieDB, clientTrieDB *trie.Database
				root                       common.Hash
				additionalRoots            []common.Hash
			)
			serverTrieDB, root, additionalRoots = test.prepareForTest(t)
			codec := getSyncCodec(t)
			leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, codec, handlerstats.NewNoopHandlerStats())
			codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), codec, handlerstats.NewNoopHandlerStats())
			mockClient := statesyncclient.NewMockClient(codec, leafsRequestHandler, codeRequestHandler, nil)

			clientDB = memorydb.New()
			s, err := NewEVMStateSyncer(&EVMStateSyncerConfig{
				Client: mockClient,
				Root:   root,
				DB:     clientDB,
			})
			if err != nil {
				t.Fatal("could not create StateSyncer", err)
			}
			// begin sync
			s.Start(context.Background())
			waitFor(t, s.Done(), test.expectedError, testSyncTimeout)

			if test.expectedError == nil {
				clientTrieDB = trie.NewDatabase(clientDB)
				test.assertSyncResult(t, testSyncResult{
					root:            root,
					additionalRoots: additionalRoots,
					serverTrieDB:    serverTrieDB,
					clientTrieDB:    clientTrieDB,
				})
			}
		})
	}
}

func fillAccountsWithStorage(t *testing.T, serverTrieDB *trie.Database, root common.Hash) common.Hash {
	return fillAccounts(t, serverTrieDB, root, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
		if index%3 == 0 {
			codeBytes := make([]byte, 256)
			_, err := rand.Read(codeBytes)
			if err != nil {
				t.Fatalf("error reading random code bytes: %v", err)
			}

			codeHash := crypto.Keccak256Hash(codeBytes)
			rawdb.WriteCode(serverTrieDB.DiskDB(), codeHash, codeBytes)
			account.CodeHash = codeHash[:]

			// now create state trie
			numKeys := 16
			account.Root, _, _ = trie.GenerateTrie(t, serverTrieDB, numKeys, wrappers.LongLen+1)
		}
		return account
	})
}

func fillAccounts(
	t *testing.T, trieDB *trie.Database, root common.Hash, accountsLen int64,
	onAccount func(*testing.T, int64, types.StateAccount, *trie.Trie) types.StateAccount,
) common.Hash {
	tr, err := trie.New(root, trieDB)
	if err != nil {
		t.Fatalf("error opening trie: %v", err)
	}
	for i := int64(0); i < accountsLen; i++ {
		acc := types.StateAccount{
			Nonce:    uint64(i),
			Balance:  big.NewInt(i % 1337),
			CodeHash: types.EmptyCodeHash[:],
			Root:     types.EmptyRootHash,
		}

		if i%5 == 0 {
			acc.Nonce += rand.Uint64()
		}

		if onAccount != nil {
			acc = onAccount(t, i, acc, tr)
		}

		accBytes, err := rlp.EncodeToBytes(acc)
		if err != nil {
			t.Fatalf("failed to rlp encode account: %v", err)
		}

		accHash := make([]byte, common.HashLength)
		if _, err := rand.Read(accHash); err != nil {
			t.Fatalf("error reading random bytes: %v", err)
		}
		if err = tr.TryUpdate(accHash, accBytes); err != nil {
			t.Fatalf("error updating trie with account, hash=%s, err=%v", common.BytesToHash(accHash), err)
		}
	}
	newRoot, _, err := tr.Commit(nil)
	if err != nil {
		t.Fatalf("error committing trie: %v", err)
	}
	if err := trieDB.Commit(newRoot, false, nil); err != nil {
		t.Fatalf("error committing trieDB: %v", err)
	}
	return newRoot
}

func getSyncCodec(t *testing.T) codec.Manager {
	codec := codec.NewDefaultManager()
	c := linearcodec.NewDefault()
	assert.NoError(t, c.RegisterType(message.BlockRequest{}))
	assert.NoError(t, c.RegisterType(message.BlockResponse{}))
	assert.NoError(t, c.RegisterType(message.LeafsRequest{}))
	assert.NoError(t, c.RegisterType(message.LeafsResponse{}))
	assert.NoError(t, c.RegisterType(message.CodeRequest{}))
	assert.NoError(t, c.RegisterType(message.CodeResponse{}))
	assert.NoError(t, codec.RegisterCodec(message.Version, c))
	return codec
}

func TestSyncerSyncsToNewRoot(t *testing.T) {
	for _, test := range []struct {
		name               string
		deleteBetweenSyncs func(common.Hash, *trie.Database) error
	}{
		{
			name: "delete_snapshot_and_code",
			deleteBetweenSyncs: func(_ common.Hash, clientTrieDB *trie.Database) error {
				db := clientTrieDB.DiskDB()
				<-snapshot.WipeSnapshot(db, false)

				// delete code
				it := db.NewIterator(rawdb.CodePrefix, nil)
				defer it.Release()
				for it.Next() {
					if len(it.Key()) != len(rawdb.CodePrefix)+common.HashLength {
						continue
					}
					if err := db.Delete(it.Key()); err != nil {
						return err
					}
				}
				return it.Error()
			},
		},
		{
			name: "delete_snapshot_and_some_trie_nodes",
			deleteBetweenSyncs: func(root common.Hash, clientTrieDB *trie.Database) error {
				// delete snapshot first
				db := clientTrieDB.DiskDB()
				<-snapshot.WipeSnapshot(db, false)

				// next delete some trie nodes
				tr, err := trie.New(root, clientTrieDB)
				if err != nil {
					return err
				}
				it := trie.NewIterator(tr.NodeIterator(nil))
				accountsWithStorage := 0
				for it.Next() {
					var acc types.StateAccount
					if err := rlp.DecodeBytes(it.Value, &acc); err != nil {
						return err
					}
					if acc.Root == types.EmptyRootHash {
						continue
					}
					accountsWithStorage++
					if accountsWithStorage%2 != 0 {
						continue
					}
					storageTrie, err := trie.New(acc.Root, clientTrieDB)
					if err != nil {
						return err
					}
					storageIt := storageTrie.NodeIterator(nil)
					storageTrieNodes := 0
					deleteBatch := clientTrieDB.DiskDB().NewBatch()
					for storageIt.Next(true) {
						if storageIt.Leaf() {
							// only delete intermediary nodes, leafs are
							// represented as logical nodes with an empty hash
							continue
						}

						storageTrieNodes++
						if storageTrieNodes%2 != 0 {
							continue
						}
						if err := deleteBatch.Delete(storageIt.Hash().Bytes()); err != nil {
							return err
						}
					}
					if err := storageIt.Error(); err != nil {
						return err
					}
					if err := deleteBatch.Write(); err != nil {
						return err
					}
				}
				return it.Err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testSyncerSyncsToNewRoot(t, test.deleteBetweenSyncs)
		})
	}
}

func testSyncerSyncsToNewRoot(t *testing.T, deleteBetweenSyncs func(common.Hash, *trie.Database) error) {
	rand.Seed(1)
	serverTrieDB := trie.NewDatabase(memorydb.New())
	root1 := fillAccountsWithStorage(t, serverTrieDB, common.Hash{})
	root2 := fillAccountsWithStorage(t, serverTrieDB, root1)

	if root1 == root2 {
		t.Fatalf("expected generated test trie roots to be different, root1=%s, root2=%s", root1, root2)
	}

	var (
		clientDB     *memorydb.Database
		clientTrieDB *trie.Database
	)
	codec := getSyncCodec(t)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewMockClient(codec, leafsRequestHandler, codeRequestHandler, nil)

	clientDB = memorydb.New()
	clientTrieDB = trie.NewDatabase(clientDB)

	s, err := NewEVMStateSyncer(&EVMStateSyncerConfig{
		Client: mockClient,
		Root:   root1,
		DB:     clientDB,
	})
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	waitFor(t, s.Done(), nil, testSyncTimeout)

	assertDBConsistency(t, root1, serverTrieDB, clientTrieDB)

	assert.True(t, mockClient.LeavesReceived() > 0)
	assert.True(t, mockClient.CodeReceived() > 0)

	if err := deleteBetweenSyncs(root1, clientTrieDB); err != nil {
		t.Fatalf("could not delete storage snapshot entry: %v", err)
	}

	// now sync to new root
	s, err = NewEVMStateSyncer(&EVMStateSyncerConfig{
		Client: mockClient,
		Root:   root2,
		DB:     clientDB,
	})
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	waitFor(t, s.Done(), nil, testSyncTimeout)

	assertDBConsistency(t, root2, serverTrieDB, clientTrieDB)
	assert.True(t, mockClient.LeavesReceived() > 0)
	assert.True(t, mockClient.CodeReceived() > 0)
}

func Test_Sync2FullEthTrieSync_ResumeFromPartialAccount(t *testing.T) {
	rand.Seed(1)
	serverTrieDB := trie.NewDatabase(memorydb.New())
	root := fillAccountsWithStorage(t, serverTrieDB, common.Hash{})

	// setup client
	clientDB := memorydb.New()
	codec := getSyncCodec(t)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewMockClient(codec, leafsRequestHandler, codeRequestHandler, nil)

	s, err := NewEVMStateSyncer(&EVMStateSyncerConfig{
		Client: mockClient,
		Root:   root,
		DB:     clientDB,
	})
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}

	// begin sync
	s.Start(context.Background())
	waitFor(t, s.Done(), nil, testSyncTimeout)

	// get the two tries and ensure they have equal nodes
	clientTrieDB := trie.NewDatabase(clientDB)
	assertDBConsistency(t, root, serverTrieDB, clientTrieDB)
}
