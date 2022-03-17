// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

type expectedSnapshot struct {
	accounts map[common.Hash]types.StateAccount
	codes    map[common.Hash][]byte
}

func newExpectedSnapshot(db ethdb.Iteratee) (*expectedSnapshot, error) {
	expected := &expectedSnapshot{
		accounts: make(map[common.Hash]types.StateAccount),
		codes:    make(map[common.Hash][]byte),
	}
	codePrefixLen := len(rawdb.CodePrefix)
	codeIt := db.NewIterator(rawdb.CodePrefix, nil)
	defer codeIt.Release()
	for codeIt.Next() {
		keyLen := len(codeIt.Key())
		if keyLen != codePrefixLen+common.HashLength {
			continue
		}
		key := common.BytesToHash(codeIt.Key()[codePrefixLen:])
		expected.codes[key] = codeIt.Value()
	}
	if err := codeIt.Error(); err != nil {
		return nil, err
	}

	prefixLen := len(rawdb.SnapshotAccountPrefix)
	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	defer it.Release()
	for it.Next() {
		keyLen := len(it.Key())
		if keyLen != prefixLen+common.HashLength {
			continue
		}
		key := common.BytesToHash(it.Key()[prefixLen:])
		fullAccount, err := snapshot.FullAccountRLP(it.Value())
		if err != nil {
			return nil, err
		}
		var account types.StateAccount
		if err := rlp.DecodeBytes(fullAccount, &account); err != nil {
			return nil, err
		}
		expected.accounts[key] = account
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return expected, nil
}

type stateSyncResult struct {
	root                       common.Hash
	syncer                     *stateSyncer
	accounts                   map[common.Hash]types.StateAccount
	serverTrieDB, clientTrieDB *trie.Database
	expectedSnapshot           *expectedSnapshot
}

func TestStateSyncerSync(t *testing.T) {
	rand.Seed(1)
	tests := map[string]struct {
		prepareForTest   func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) // return trie database and trie root to sync
		keyLen           int                                                                                  // size of keys, default: 32
		assertSyncResult func(t *testing.T, result stateSyncResult)
		expectError      bool
		assertError      func(t *testing.T, err error)
	}{
		"accounts only trie": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening server trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, nil)
				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}
				return serverTrieDB, accounts, root
			},
			assertSyncResult: func(t *testing.T, result stateSyncResult) {
				trie.AssertTrieConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB, nil)

				// assert snapshot has correct values
				assert.Equal(t, len(result.accounts), len(result.expectedSnapshot.accounts))
				assert.Len(t, result.expectedSnapshot.accounts, 1000)
				assert.Len(t, result.expectedSnapshot.codes, 0)

				for hash := range result.accounts {
					if _, exists := result.expectedSnapshot.accounts[hash]; !exists {
						t.Fatalf("expected snapshot to contain account has [%s]", hash)
					}
				}
			},
		},
		"accounts with codes trie": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening server trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, func(t *testing.T, index int64, account types.StateAccount, trie *trie.Trie) types.StateAccount {
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
				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}
				return serverTrieDB, accounts, root
			},
			assertSyncResult: func(t *testing.T, result stateSyncResult) {
				trie.AssertTrieConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB, nil)

				// assert snapshot has correct values
				assert.Equal(t, len(result.accounts), len(result.expectedSnapshot.accounts))
				assert.Len(t, result.expectedSnapshot.accounts, 1000)
				assert.Equal(t, (1000/3)+1, len(result.expectedSnapshot.codes))

				for hash := range result.accounts {
					if _, exists := result.expectedSnapshot.accounts[hash]; !exists {
						t.Fatalf("expected snapshot to contain account has [%s]", hash)
					}
				}
			},
		},
		"missing sync root": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())

				return serverTrieDB, map[common.Hash]types.StateAccount{}, common.BytesToHash([]byte("totally-fake-root"))
			},
			expectError: true,
			assertError: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "failed to fetch leafs")
			},
		},
		"empty server trie": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				return trie.NewDatabase(memorydb.New()), map[common.Hash]types.StateAccount{}, common.BytesToHash([]byte("some empty bytes"))
			},
			expectError: true,
			assertError: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "failed to fetch leafs")
			},
		},
		"inconsistent server trie": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening server trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, nil)
				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				diskDB := serverTrieDB.DiskDB()

				// delete some random entries from the diskDB
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
				return serverTrieDB, accounts, root
			},
			expectError: true,
			assertError: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "failed to fetch leafs")
			},
		},
		"sync non latest root": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening server trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, nil)
				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				// add new accounts
				accountsLen := len(accounts)
				for i := accountsLen; i < accountsLen+500; i++ {
					acc := types.StateAccount{
						Nonce:    uint64(i),
						Balance:  big.NewInt(int64(i % 1337)),
						CodeHash: types.EmptyCodeHash[:],
						Root:     types.EmptyRootHash,
					}

					accBytes, err := rlp.EncodeToBytes(acc)
					if err != nil {
						t.Fatalf("failed to rlp encode account: %v", err)
					}

					hash := crypto.Keccak256(accBytes)
					if err = serverTrie.TryUpdate(hash, accBytes); err != nil {
						t.Fatalf("error updating trie with account, hash=%s, err=%v", hash, err)
					}

					accounts[common.BytesToHash(hash)] = acc
				}

				newRoot, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit server trie: %v", err)
				}

				if err = serverTrieDB.Commit(newRoot, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", newRoot, err)
				}

				return serverTrieDB, accounts, root
			},
			expectError: false,
			assertSyncResult: func(t *testing.T, result stateSyncResult) {
				// ensure tries are consistent
				trie.AssertTrieConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB, nil)

				clientTrie, err := trie.New(result.root, result.clientTrieDB)
				if err != nil {
					t.Fatalf("error opening client trie, root=%s, err=%v", result.root, err)
				}

				foundHash := make(map[common.Hash]struct{}, 1000)
				clientTrieIter := trie.NewIterator(clientTrie.NodeIterator(nil))
				for clientTrieIter.Next() {
					hash := common.BytesToHash(clientTrieIter.Key)
					foundHash[hash] = struct{}{}
				}

				notFound := 0
				for hash := range result.accounts {
					if _, exists := foundHash[hash]; !exists {
						notFound++
					}
				}

				assert.EqualValues(t, 500, notFound)
			},
		},
		"malformed account": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, nil)

				// input one malformed account
				accountBytes := []byte("some malformed account is here yo")
				accountHash := crypto.Keccak256Hash(accountBytes)
				if err = serverTrie.TryUpdate(accountHash[:], accountBytes); err != nil {
					t.Fatalf("error updating server trie: %v", err)
				}

				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				return serverTrieDB, accounts, root
			},
			expectError: true,
			assertError: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "rlp: expected input list for types.StateAccount")
			},
		},
		"accounts with storage": {
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening trie: %v", err)
				}

				accounts := fillAccountsWithStorage(t, serverTrie, serverTrieDB)

				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				return serverTrieDB, accounts, root
			},
			assertSyncResult: func(t *testing.T, result stateSyncResult) {
				trie.AssertTrieConsistency(t, result.root, result.serverTrieDB, result.clientTrieDB, nil)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var (
				serverTrieDB, clientTrieDB *trie.Database
				accounts                   map[common.Hash]types.StateAccount
				root                       common.Hash
			)
			serverTrieDB, accounts, root = test.prepareForTest(t)
			codec, err := message.BuildCodec()
			assert.NoError(t, err)

			mockClient := syncclient.NewMockClient(
				codec,
				handlers.NewLeafsRequestHandler(serverTrieDB, codec, handlerstats.NewNoopHandlerStats()),
				handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), codec, handlerstats.NewNoopHandlerStats()),
				nil,
			)

			clientDB := memorydb.New()

			syncer, err := NewEVMStateSyncer(&EVMStateSyncerConfig{
				Client: mockClient,
				Root:   root,
				DB:     clientDB,
			})
			if err != nil {
				t.Fatalf("error creating new state syncer: %v", err)
			}

			syncer.Start(context.Background())
			err = <-syncer.Done()

			isError := err != nil
			if isError != test.expectError {
				t.Fatalf("unexpected error in test, err=%v", err)
			} else if test.expectError {
				assert.Error(t, err)
				test.assertError(t, err)
			} else {
				clientTrieDB = trie.NewDatabase(clientDB)
				expectedSnapshot, err := newExpectedSnapshot(clientDB)
				if err != nil {
					t.Fatal("could not parse snapshot", err)
				}
				test.assertSyncResult(t, stateSyncResult{
					root:             root,
					accounts:         accounts,
					serverTrieDB:     serverTrieDB,
					clientTrieDB:     clientTrieDB,
					syncer:           syncer,
					expectedSnapshot: expectedSnapshot,
				})
			}
		})
	}
}
