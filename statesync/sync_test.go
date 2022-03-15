// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers"
	handlerstats "github.com/ava-labs/coreth/statesync/handlers/stats"
	syncerstats "github.com/ava-labs/coreth/statesync/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	canaryHash      = common.HexToHash("0x2f036f99d0917de46ff7e399f62bd629e6bc9764a5c6f2fb52fe494ff0c63f56")
	hashedCanaryKey = crypto.Keccak256Hash([]byte("hello this is"))
)

func makeStateTrie(t *testing.T, accountsLen, commitFrequency uint) (map[common.Hash]types.StateAccount, []common.Hash, ethdb.KeyValueStore, *trie.Database) {
	rand.Seed(1)
	assert.Greater(t, accountsLen, commitFrequency, "number of accounts must be greater than commit frequency to generate test state")
	roots := make([]common.Hash, accountsLen%commitFrequency)
	accounts := make(map[common.Hash]types.StateAccount, accountsLen)
	db := memorydb.New()
	trieDB := trie.NewDatabase(db)
	tree, err := trie.New(common.Hash{}, trieDB)
	assert.NoError(t, err)
	assert.NotNil(t, tree)

	// produce a canary account which is consistent and can be relied upon for testing
	acc := types.StateAccount{
		Nonce:    2,
		Balance:  big.NewInt(100000),
		CodeHash: types.EmptyCodeHash[:],
	}

	// produce storage root
	storageTree, err := trie.NewSecure(common.Hash{}, trieDB)
	assert.NoError(t, err)
	storageTree.Update([]byte("hello this is"), []byte("canary"))
	root, _, err := storageTree.Commit(nil)
	assert.NoError(t, err)
	acc.Root = root

	// encode the account and update the trie
	b, err := rlp.EncodeToBytes(acc)
	assert.NoError(t, err)
	hash := crypto.Keccak256Hash(b)
	tree.Update(hash[:], b) // hash = 0x009494c23cd024da48d009f4de322c2fcac23dbb60d016fe8afcc3fabf920005
	accounts[hash] = acc

	for i := uint(1); i < accountsLen; i++ {
		acc = types.StateAccount{
			Nonce:    uint64(i % 15),
			Balance:  big.NewInt(int64(i % 10)),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash[:],
		}

		if i%3 == 0 {
			codeData := []byte{byte(i % 100)}
			if i%10000 == 0 {
				codeData = utils.RandomBytes(64)
			}
			codeHash := crypto.Keccak256Hash(codeData)
			acc.CodeHash = codeHash.Bytes()
			rawdb.WriteCode(db, codeHash, codeData)
		} else if i%6 == 0 {
			// create storage trie
			storageTree, err = trie.NewSecure(common.Hash{}, trieDB)
			assert.NoError(t, err)
			storageTree.Update(append([]byte("storagekey1-"), byte(i%100)), append([]byte("storagevalue1-"), byte(i%100)))
			storageTree.Update(append([]byte("storagekey2-"), byte(i%100)), append([]byte("storagevalue2-"), byte(i%100)))

			if i%100 == 0 {
				storageTree.Update(utils.RandomBytes(16), utils.RandomBytes(32))
				storageTree.Update(utils.RandomBytes(16), utils.RandomBytes(32))
				storageTree.Update(utils.RandomBytes(16), utils.RandomBytes(32))
			}

			root, _, err := storageTree.Commit(nil)
			assert.NoError(t, err)
			acc.Root = root
		}

		b, err := rlp.EncodeToBytes(acc)
		assert.NoError(t, err)
		accountHash := crypto.Keccak256Hash(b)
		tree.Update(accountHash[:], b)

		if i%commitFrequency == 0 {
			hash, _, err := tree.Commit(nil)
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, hash)
			roots = append(roots, hash)
		}

		accounts[accountHash] = acc
	}

	hash, _, err = tree.Commit(nil)
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, hash)
	roots = append(roots, hash)

	return accounts, roots, db, trieDB
}

type testSyncResult struct {
	root                       common.Hash
	syncer                     *stateSyncer
	accounts                   map[common.Hash]types.StateAccount
	serverTrieDB, clientTrieDB *trie.Database
	syncerStats                *syncerstats.MockSyncerStats
}

func TestSyncer(t *testing.T) {
	rand.Seed(1)
	tests := []struct {
		name             string
		prepareForTest   func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) // return trie database and trie root to sync
		assertSyncResult func(t *testing.T, result testSyncResult)
		expectedError    error
	}{
		{
			name: "accounts_only_trie",
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
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertAccountsTrieConsistency(t, result)
			},
		},
		{
			name: "missing_sync_root",
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())

				return serverTrieDB, map[common.Hash]types.StateAccount{}, common.BytesToHash([]byte("totally-fake-root"))
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "empty_server_trie",
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				return trie.NewDatabase(memorydb.New()), map[common.Hash]types.StateAccount{}, common.BytesToHash([]byte("some empty bytes"))
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "inconsistent_server_trie",
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
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "sync_non-latest_root",
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
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				// ensure tries are consistent
				assertAccountsTrieConsistency(t, result)

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
		{
			name: "malformed_account",
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
			expectedError: errors.New("rlp: expected input list for types.StateAccount"),
		},
		{
			name: "accounts_with_storage",
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
			assertSyncResult: func(t *testing.T, result testSyncResult) {
				assertAccountsTrieConsistency(t, result)
			},
		},
		{
			name: "accounts_with_missing_storage",
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
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

				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				return serverTrieDB, accounts, root
			},
			expectedError: errors.New("failed to fetch leafs"),
		},
		{
			name: "accounts_with_missing_code",
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
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

				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				return serverTrieDB, accounts, root
			},
			expectedError: errors.New("error getting code bytes for code hash"),
		},
		{
			name: "code_hash_mismatch",
			prepareForTest: func(t *testing.T) (*trie.Database, map[common.Hash]types.StateAccount, common.Hash) {
				serverTrieDB := trie.NewDatabase(memorydb.New())
				serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
				if err != nil {
					t.Fatalf("error opening trie: %v", err)
				}

				accounts := fillAccounts(t, serverTrie, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
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

				root, _, err := serverTrie.Commit(nil)
				if err != nil {
					t.Fatalf("could not commit trie: %v", err)
				}

				if err = serverTrieDB.Commit(root, false, nil); err != nil {
					t.Fatalf("error committing server trie DB, root=%s, err=%v", root, err)
				}

				return serverTrieDB, accounts, root
			},
			expectedError: errors.New("error getting code bytes for code hash"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				clientDB                   *memorydb.Database
				serverTrieDB, clientTrieDB *trie.Database
				accounts                   map[common.Hash]types.StateAccount
				root                       common.Hash
			)
			serverTrieDB, accounts, root = test.prepareForTest(t)
			codec := getSyncCodec(t)
			leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
			codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), handlerstats.NewNoopHandlerStats(), codec)
			client := NewMockLeafClient(codec, leafsRequestHandler, codeRequestHandler, nil)

			clientDB = memorydb.New()
			syncerStats := &syncerstats.MockSyncerStats{}
			s, err := NewStateSyncer(root, client, 4, syncerStats, clientDB, commitCap)
			if err != nil {
				t.Fatal("could not create StateSyncer", err)
			}
			// begin sync
			s.Start(context.Background())
			waitFor(t, s.Done(), test.expectedError, testSyncTimeout)

			if test.expectedError == nil {
				clientTrieDB = trie.NewDatabase(clientDB)
				test.assertSyncResult(t, testSyncResult{
					root:         root,
					accounts:     accounts,
					serverTrieDB: serverTrieDB,
					clientTrieDB: clientTrieDB,
					syncer:       s,
					syncerStats:  syncerStats,
				})
			}
		})
	}
}

func fillAccountsWithStorage(t *testing.T, serverTrie *trie.Trie, serverTrieDB *trie.Database) map[common.Hash]types.StateAccount {
	return fillAccounts(t, serverTrie, 1000, func(t *testing.T, index int64, account types.StateAccount, tr *trie.Trie) types.StateAccount {
		if index%3 == 0 {
			codeBytes := make([]byte, 256)
			_, err := rand.Read(codeBytes)
			if err != nil {
				t.Fatalf("error reading random code bytes: %v", err)
			}

			codeHash := crypto.Keccak256Hash(codeBytes)
			rawdb.WriteCode(serverTrieDB.DiskDB(), codeHash, codeBytes)

			// now create state trie
			stateTrie, err := trie.NewSecure(common.Hash{}, serverTrieDB)
			if err != nil {
				t.Fatalf("error opening storage trie: %v", err)
			}
			for i := index; i < index+rand.Int63n(32)+2; i++ {
				prefix := make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(prefix, uint64(i))
				if err = stateTrie.TryUpdate(prefix, []byte(fmt.Sprintf("some value %d", i))); err != nil {
					t.Fatalf("error updating state trie: %v", err)
				}
			}

			stateRoot, _, err := stateTrie.Commit(nil)
			if err != nil {
				t.Fatalf("error committing state trie: %v", err)
			}

			account.CodeHash = codeHash[:]
			account.Root = stateRoot
		}
		return account
	})
}

// assertAccountsTrieConsistency ensures given serverTrieDB has same entries in the same order as clientTrieDB at given root
func assertAccountsTrieConsistency(t *testing.T, result testSyncResult) {
	serverTrie, err := trie.New(result.root, result.serverTrieDB)
	if err != nil {
		t.Fatalf("error creating server trie, root=%s, err=%v", result.root, err)
	}
	clientTrie, err := trie.New(result.root, result.clientTrieDB)
	if err != nil {
		t.Fatalf("error creating client trie, root=%s, err=%v", result.root, err)
	}

	serverTrieIter := trie.NewIterator(serverTrie.NodeIterator(nil))
	clientTrieIter := trie.NewIterator(clientTrie.NodeIterator(nil))
	count := 0
	for serverTrieIter.Next() && clientTrieIter.Next() {
		count++
		assert.Equal(t, serverTrieIter.Key, clientTrieIter.Key)
		assert.Equal(t, serverTrieIter.Value, clientTrieIter.Value)

		hash := common.BytesToHash(serverTrieIter.Key)
		acc := result.accounts[hash]

		if acc.CodeHash == nil || common.BytesToHash(acc.CodeHash) == types.EmptyCodeHash {
			continue
		}

		serverCodeBytes := rawdb.ReadCode(result.serverTrieDB.DiskDB(), common.BytesToHash(acc.CodeHash))
		clientCodeBytes := rawdb.ReadCode(result.clientTrieDB.DiskDB(), common.BytesToHash(acc.CodeHash))
		assert.NotEmpty(t, serverCodeBytes)
		assert.Equal(t, serverCodeBytes, clientCodeBytes)

		if acc.Root == types.EmptyRootHash {
			continue
		}

		serverStorageTrie, err := trie.New(acc.Root, result.serverTrieDB)
		if err != nil {
			t.Fatalf("could not open storage trie on server, root=%s, err=%v", acc.Root, err)
		}
		clientStorageTrie, err := trie.New(acc.Root, result.clientTrieDB)
		if err != nil {
			t.Fatalf("could not open storage trie on client, root=%s, err=%v", acc.Root, err)
		}

		serverStorageTrieIter := trie.NewIterator(serverStorageTrie.NodeIterator(nil))
		clientStorageTrieIter := trie.NewIterator(clientStorageTrie.NodeIterator(nil))
		for serverStorageTrieIter.Next() && clientStorageTrieIter.Next() {
			assert.Equal(t, serverStorageTrieIter.Key, clientStorageTrieIter.Key)
			assert.Equal(t, serverStorageTrieIter.Value, clientStorageTrieIter.Value)
		}
	}
}

func fillAccounts(t *testing.T, tr *trie.Trie, accountsLen int64, onAccount func(*testing.T, int64, types.StateAccount, *trie.Trie) types.StateAccount) map[common.Hash]types.StateAccount {
	accounts := make(map[common.Hash]types.StateAccount, accountsLen)
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

		hash := crypto.Keccak256(accBytes)
		if err = tr.TryUpdate(hash, accBytes); err != nil {
			t.Fatalf("error updating trie with account, hash=%s, err=%v", hash, err)
		}

		accounts[common.BytesToHash(hash)] = acc
	}

	return accounts
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
	serverTrieDB := trie.NewDatabase(memorydb.New())
	serverTrie, err := trie.New(common.Hash{}, serverTrieDB)
	assert.NoError(t, err)

	accounts := fillAccountsWithStorage(t, serverTrie, serverTrieDB)
	root1, _, err := serverTrie.Commit(nil)
	assert.NoError(t, err)
	err = serverTrieDB.Commit(root1, false, nil)
	assert.NoError(t, err)

	serverTrie, err = trie.New(root1, serverTrieDB)
	assert.NoError(t, err)
	{
		accounts2 := fillAccountsWithStorage(t, serverTrie, serverTrieDB)
		// add all new accounts to original accounts map
		for hash, account := range accounts2 {
			accounts[hash] = account
		}
	}

	root2, _, err := serverTrie.Commit(nil)
	assert.NoError(t, err)
	err = serverTrieDB.Commit(root2, false, nil)
	assert.NoError(t, err)

	if root1 == root2 {
		t.Fatalf("expected generated test trie roots to be different, root1=%s, root2=%s, accountsLen=%d", root1, root2, len(accounts))
	}

	var (
		clientDB     *memorydb.Database
		clientTrieDB *trie.Database
	)
	codec := getSyncCodec(t)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), handlerstats.NewNoopHandlerStats(), codec)
	client := NewMockLeafClient(codec, leafsRequestHandler, codeRequestHandler, nil)

	clientDB = memorydb.New()
	clientTrieDB = trie.NewDatabase(clientDB)

	syncerStats := &syncerstats.MockSyncerStats{}
	s, err := NewStateSyncer(root1, client, 4, syncerStats, clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	waitFor(t, s.Done(), nil, testSyncTimeout)

	assertAccountsTrieConsistency(t, testSyncResult{
		root:         root1,
		accounts:     accounts,
		serverTrieDB: serverTrieDB,
		clientTrieDB: clientTrieDB,
		syncer:       s,
	})

	assert.True(t, syncerStats.LeavesReceived > 0)
	assert.True(t, syncerStats.LeavesRequested > 0)

	// TODO: fix stats
	// assert.True(t, syncerStats.CodeCommitted > 0)
	// assert.True(t, syncerStats.StorageCommitted > 0)
	// assert.True(t, syncerStats.TrieCommitted > 0)

	db := clientTrieDB.DiskDB()

	// delete all account snapshot entries
	iter := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	for iter.Next() {
		if err = db.Delete(iter.Key()); err != nil {
			t.Fatalf("could not delete account snapshot entry: %v", err)
		}
	}
	iter.Release()

	// delete all storage snapshot entries
	iter = db.NewIterator(rawdb.SnapshotStoragePrefix, nil)
	for iter.Next() {
		if err = db.Delete(iter.Key()); err != nil {
			t.Fatalf("could not delete storage snapshot entry: %v", err)
		}
	}
	iter.Release()

	// delete all codes
	iter = db.NewIterator(rawdb.CodePrefix, nil)
	for iter.Next() {
		if err = db.Delete(iter.Key()); err != nil {
			t.Fatalf("could not delete storage snapshot entry: %v", err)
		}
	}
	iter.Release()

	// reset syncer stats
	syncerStats = &syncerstats.MockSyncerStats{}
	// now sync to new root
	s, err = NewStateSyncer(root2, client, 4, syncerStats, clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}
	// begin sync
	s.Start(context.Background())
	waitFor(t, s.Done(), nil, testSyncTimeout)

	if _, err = trie.New(root2, clientTrieDB); err != nil {
		t.Fatal(err)
	}

	assertAccountsTrieConsistency(t, testSyncResult{
		root:         root2,
		accounts:     accounts,
		serverTrieDB: serverTrieDB,
		clientTrieDB: clientTrieDB,
		syncer:       s,
	})

	assert.True(t, syncerStats.LeavesReceived > 0)
	assert.True(t, syncerStats.LeavesRequested > 0)

	// TODO: fix stats
	// assert.True(t, syncerStats.CodeCommitted > 0)
	// assert.True(t, syncerStats.StorageCommitted > 0)
	// assert.True(t, syncerStats.TrieCommitted > 0)
}

func Test_Sync2FullEthTrieSync_ResumeFromPartialAccount(t *testing.T) {
	accounts, roots, _, serverTrieDB := makeStateTrie(t, 10_000, 1000) // _ = accounts
	root := roots[len(roots)-1]

	serverTrie, err := trie.New(root, serverTrieDB)
	assert.NoError(t, err)

	// setup client
	clientDB := memorydb.New()
	codec := getSyncCodec(t)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, handlerstats.NewNoopHandlerStats(), codec)
	codeRequestHandler := handlers.NewCodeRequestHandler(serverTrieDB.DiskDB(), handlerstats.NewNoopHandlerStats(), codec)
	client := NewMockLeafClient(codec, leafsRequestHandler, codeRequestHandler, nil)

	s, err := NewStateSyncer(root, client, 4, syncerstats.NewNoOpStats(), clientDB, commitCap)
	if err != nil {
		t.Fatal("could not create StateSyncer", err)
	}

	canaryAccount, exists := accounts[canaryHash]
	assert.True(t, exists)

	canaryStorage, err := serverTrieDB.Node(canaryAccount.Root)
	assert.NoError(t, err)
	assert.Greater(t, len(canaryStorage), 0)
	err = clientDB.Put(canaryHash[:], canaryStorage)
	assert.NoError(t, err)

	// begin sync
	s.Start(context.Background())
	waitFor(t, s.Done(), nil, testSyncTimeout)

	// get the two tries and ensure they have equal nodes
	clientTrieDB := trie.NewDatabase(clientDB)
	clientTrie, err := trie.New(root, clientTrieDB)
	assert.NoError(t, err, "client trie must initialise with synced root")

	// ensure storage root can be initialized
	for _, acc := range accounts {
		if acc.Root == types.EmptyRootHash {
			continue
		}

		trie.AssertTrieConsistency(t, acc.Root, serverTrieDB, clientTrieDB)

		clientStorage, err := trie.NewSecure(acc.Root, clientTrieDB)
		assert.NoError(t, err, "client must have storage root")
		clientStorageIter := trie.NewIterator(clientStorage.NodeIterator(nil))
		canaryFound := false
		for clientStorageIter.Next() {
			if common.BytesToHash(clientStorageIter.Key) == hashedCanaryKey {
				canaryFound = true
			}
		}
		assert.True(t, canaryFound, "expected to find the canary accounts")

		if common.BytesToHash(acc.CodeHash) != types.EmptyCodeHash {
			serverCodeData := serverTrie.Get(acc.CodeHash)
			assert.NotEmpty(t, serverCodeData)
			clientCodeData := clientTrie.Get(acc.CodeHash)
			assert.NotEmpty(t, clientCodeData)
			assert.True(t, bytes.Equal(serverCodeData, clientCodeData))
		}
	}

	// ensure trie hashes are the same
	assert.Equal(t, serverTrie.Hash(), clientTrie.Hash(), "server trie hash and client trie hash must match")
	trie.AssertTrieConsistency(t, root, serverTrieDB, clientTrieDB)
}
