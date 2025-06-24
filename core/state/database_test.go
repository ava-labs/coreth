// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package state

import (
	"encoding/binary"
	"math/rand"
	"slices"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/triedb/firewood"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	commit byte = iota
	createAccount
	updateAccount
	deleteAccount
	addStorage
	updateStorage
	deleteStorage
	maxStep
)

var (
	stepMap = map[byte]string{
		commit:        "commit",
		createAccount: "createAccount",
		updateAccount: "updateAccount",
		deleteAccount: "deleteAccount",
		addStorage:    "addStorage",
		updateStorage: "updateStorage",
		deleteStorage: "deleteStorage",
	}
)

type fuzzState struct {
	require *require.Assertions

	// current state
	currentAddrs               []common.Address
	currentStorageInputIndices map[common.Address]uint64
	inputCounter               uint64
	blockNumber                uint64

	// pending changes to be committed
	merkleTries []*merkleTrie
}
type merkleTrie struct {
	ethDatabase      Database
	accountTrie      Trie
	openStorageTries map[common.Address]Trie
	lastRoot         common.Hash
}

func newFuzzState(t *testing.T) *fuzzState {
	r := require.New(t)

	hashState := NewDatabaseWithConfig(
		rawdb.NewMemoryDatabase(),
		&triedb.Config{
			DBOverride: hashdb.Defaults.BackendConstructor,
		})
	ethRoot := types.EmptyRootHash
	hashTr, err := hashState.OpenTrie(ethRoot)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(hashState.TrieDB().Close())
	})

	firewoodMemdb := rawdb.NewMemoryDatabase()
	r.NoError(customrawdb.WriteChainDataPath(firewoodMemdb, t.TempDir()))
	firewoodState := NewDatabaseWithConfig(
		firewoodMemdb,
		&triedb.Config{
			DBOverride: firewood.Defaults.BackendConstructor,
		},
	)
	fwTr, err := firewoodState.OpenTrie(ethRoot)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(firewoodState.TrieDB().Close())
	})

	return &fuzzState{
		merkleTries: []*merkleTrie{
			&merkleTrie{
				ethDatabase:      hashState,
				accountTrie:      hashTr,
				openStorageTries: make(map[common.Address]Trie),
				lastRoot:         ethRoot,
			},
			&merkleTrie{
				ethDatabase:      firewoodState,
				accountTrie:      fwTr,
				openStorageTries: make(map[common.Address]Trie),
				lastRoot:         ethRoot,
			},
		},
		currentStorageInputIndices: make(map[common.Address]uint64),
		require:                    r,
	}
}

// commit writes the pending changes to both tries and clears the pending changes
func (fs *fuzzState) commit() {
	for _, tr := range fs.merkleTries {
		mergedNodeSet := trienode.NewMergedNodeSet()
		for addr, str := range tr.openStorageTries {
			accountStateRoot, set, err := str.Commit(false)
			fs.require.NoError(err)
			// A no-op change returns a nil set, which will cause merge to panic.
			if set != nil {
				fs.require.NoError(mergedNodeSet.Merge(set))
			}

			acc, err := tr.accountTrie.GetAccount(addr)
			fs.require.NoError(err)
			// If the account was deleted, we can skip updating the account's
			// state root.
			fs.require.NotNil(acc)

			acc.Root = accountStateRoot
			fs.require.NoError(tr.accountTrie.UpdateAccount(addr, acc))
		}

		updatedRoot, set, err := tr.accountTrie.Commit(true)
		fs.require.NoError(err)

		// A no-op change returns a nil set, which will cause merge to panic.
		if set != nil {
			fs.require.NoError(mergedNodeSet.Merge(set))
		}

		if updatedRoot != tr.lastRoot {
			triedbopt := stateconf.WithTrieDBUpdatePayload(common.Hash{byte(fs.blockNumber - 1)}, common.Hash{byte(fs.blockNumber)})
			fs.require.NoError(tr.ethDatabase.TrieDB().Update(updatedRoot, tr.lastRoot, fs.blockNumber, mergedNodeSet, nil), stateconf.WithTrieDBUpdateOpts(triedbopt))
			tr.lastRoot = updatedRoot
		}
		tr.openStorageTries = make(map[common.Address]Trie)
		fs.require.NoError(tr.ethDatabase.TrieDB().Commit(updatedRoot, true),
			"expected hashdb root %s", fs.merkleTries[0].lastRoot.Hex())
		tr.accountTrie, err = tr.ethDatabase.OpenTrie(tr.lastRoot)
		fs.require.NoError(err)
	}
	fs.blockNumber++

	// After computing the new root for each trie, we can confirm that the hashing matches
	expectedRoot := fs.merkleTries[0].lastRoot
	for i, tr := range fs.merkleTries[1:] {
		fs.require.Equalf(expectedRoot, tr.lastRoot,
			"expected root %x, got %x for trie %d",
			expectedRoot.Hex(), tr.lastRoot.Hex(), i,
		)
	}
}

// createAccount generates a new, unique account and adds it to both tries and the tracked
// current state.
func (fs *fuzzState) createAccount() {
	fs.inputCounter++
	addr := common.BytesToAddress(crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, fs.inputCounter)).Bytes())
	acc := &types.StateAccount{
		Nonce:    1,
		Balance:  uint256.NewInt(100),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash[:],
	}
	fs.currentAddrs = append(fs.currentAddrs, addr)

	for _, tr := range fs.merkleTries {
		fs.require.NoError(tr.accountTrie.UpdateAccount(addr, acc))
	}
}

// selectAccount returns a random account and account hash for the provided index
// assumes: addrIndex < len(tr.currentAddrs)
func (fs *fuzzState) selectAccount(addrIndex int) common.Address {
	return fs.currentAddrs[addrIndex]
}

// updateAccount selects a random account, increments its nonce, and adds the update
// to the pending changes for both tries.
func (fs *fuzzState) updateAccount(addrIndex int) {
	addr := fs.selectAccount(addrIndex)

	for _, tr := range fs.merkleTries {
		acc, err := tr.accountTrie.GetAccount(addr)
		fs.require.NoError(err)
		fs.require.NotNil(acc)
		acc.Nonce++
		acc.CodeHash = crypto.Keccak256Hash(acc.CodeHash[:]).Bytes()
		acc.Balance.Add(acc.Balance, uint256.NewInt(3))
		fs.require.NoError(tr.accountTrie.UpdateAccount(addr, acc))
	}
}

// deleteAccount selects a random account and deletes it from both tries and the tracked
// current state.
func (fs *fuzzState) deleteAccount(accountIndex int) {
	deleteAddr := fs.selectAccount(accountIndex)
	fs.currentAddrs = slices.DeleteFunc(fs.currentAddrs, func(addr common.Address) bool {
		return deleteAddr == addr
	})
	for _, tr := range fs.merkleTries {
		fs.require.NoError(tr.accountTrie.DeleteAccount(deleteAddr))
		delete(tr.openStorageTries, deleteAddr) // remove any open storage trie for the deleted account
	}
}

// openStorageTrie opens the storage trie for the provided account address.
// Uses an already opened trie, if there's a pending update to the ethereum nested
// storage trie.
//
// must maintain a map of currently open storage tries, so we can defer committing them
// until commit as opposed to after each storage update.
// This mimics the actual handling of state commitments in the EVM where storage tries are all committed immediately
// before updating the account trie along with the updated storage trie roots:
// https://github.com/ava-labs/libevm/blob/0bfe4a0380c86d7c9bf19fe84368b9695fcb96c7/core/state/statedb.go#L1155
//
// If we attempt to commit the storage tries after each operation, then attempting to re-open the storage trie
// with an updated storage trie root from ethDatabase will fail since the storage trie root will not have been
// persisted yet - leading to a missing trie node error.
func (fs *fuzzState) openStorageTrie(addr common.Address, tr *merkleTrie) Trie {
	storageTrie, ok := tr.openStorageTries[addr]
	if ok {
		return storageTrie
	}

	acc, err := tr.accountTrie.GetAccount(addr)
	fs.require.NoError(err)
	fs.require.NotNil(acc, addr.Hex())
	storageTrie, err = tr.ethDatabase.OpenStorageTrie(tr.lastRoot, addr, acc.Root, tr.accountTrie)
	fs.require.NoError(err)
	tr.openStorageTries[addr] = storageTrie
	return storageTrie
}

// addStorage selects an account and adds a new storage key-value pair to the account.
func (fs *fuzzState) addStorage(accountIndex int) {
	addr := fs.selectAccount(accountIndex)
	// Increment storageInputIndices for the account and take the next input to generate
	// a new storage key-value pair for the account.
	fs.currentStorageInputIndices[addr]++
	storageIndex := fs.currentStorageInputIndices[addr]
	key := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	keyHash := crypto.Keccak256Hash(key[:])
	val := crypto.Keccak256Hash(keyHash[:])

	for _, tr := range fs.merkleTries {
		str := fs.openStorageTrie(addr, tr)
		fs.require.NoError(str.UpdateStorage(addr, key[:], val[:]))
	}

	fs.currentStorageInputIndices[addr]++
}

// updateStorage selects an account and updates an existing storage key-value pair
// note: this may "update" a key-value pair that doesn't exist if it was previously deleted.
func (fs *fuzzState) updateStorage(accountIndex int, storageIndexInput uint64) {
	addr := fs.selectAccount(accountIndex)
	storageIndex := fs.currentStorageInputIndices[addr]
	storageIndex %= storageIndexInput

	storageKey := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	storageKeyHash := crypto.Keccak256Hash(storageKey[:])
	fs.inputCounter++
	updatedValInput := binary.BigEndian.AppendUint64(storageKeyHash[:], fs.inputCounter)
	updatedVal := crypto.Keccak256Hash(updatedValInput[:])

	for _, tr := range fs.merkleTries {
		str := fs.openStorageTrie(addr, tr)
		fs.require.NoError(str.UpdateStorage(addr, storageKey[:], updatedVal[:]))
	}
}

// deleteStorage selects an account and deletes an existing storage key-value pair
// note: this may "delete" a key-value pair that doesn't exist if it was previously deleted.
func (fs *fuzzState) deleteStorage(accountIndex int, storageIndexInput uint64) {
	addr := fs.selectAccount(accountIndex)
	storageIndex := fs.currentStorageInputIndices[addr]
	storageIndex %= storageIndexInput
	storageKey := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))

	for _, tr := range fs.merkleTries {
		str := fs.openStorageTrie(addr, tr)
		fs.require.NoError(str.DeleteStorage(addr, storageKey[:]))
	}
}

func FuzzTree(f *testing.F) {
	for randSeed := range int64(1000) {
		rand := rand.New(rand.NewSource(randSeed))
		steps := make([]byte, 32)
		_, err := rand.Read(steps)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(randSeed, steps)
	}
	f.Fuzz(func(t *testing.T, randSeed int64, byteSteps []byte) {
		fuzzState := newFuzzState(t)
		rand := rand.New(rand.NewSource(randSeed))

		for range 10 {
			fuzzState.createAccount()
		}
		fuzzState.commit()

		const maxSteps = 1000
		if len(byteSteps) > maxSteps {
			byteSteps = byteSteps[:maxSteps]
		}

		for _, step := range byteSteps {
			step = step % maxStep
			t.Log(stepMap[step])
			switch step {
			case commit:
				fuzzState.commit()
			case createAccount:
				fuzzState.createAccount()
			case updateAccount:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.updateAccount(rand.Intn(len(fuzzState.currentAddrs)))
				}
			case deleteAccount:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.deleteAccount(rand.Intn(len(fuzzState.currentAddrs)))
				}
			case addStorage:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.addStorage(rand.Intn(len(fuzzState.currentAddrs)))
				}
			case updateStorage:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.updateStorage(rand.Intn(len(fuzzState.currentAddrs)), rand.Uint64())
				}
			case deleteStorage:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.deleteStorage(rand.Intn(len(fuzzState.currentAddrs)), rand.Uint64())
				}
			default:
				t.Fatalf("unknown step: %d", step)
			}
		}
	})
}
