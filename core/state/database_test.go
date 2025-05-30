package state

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"path"
	"slices"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/triedb/firewood"
	ffi "github.com/ava-labs/firewood-go/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func hashData(input []byte) common.Hash {
	return crypto.Keccak256Hash(input)
}

func TestInsert(t *testing.T) {
	tempdir := t.TempDir()
	file := path.Join(tempdir, "test.db")
	cfg := ffi.DefaultConfig()
	cfg.Create = true
	db, err := ffi.New(file, cfg)
	require.NoError(t, err)
	defer db.Close()

	type storageKey struct {
		addr common.Address
		key  common.Hash
	}

	rand := rand.New(rand.NewSource(0))

	addrs := make([]common.Address, 0)
	storages := make([]storageKey, 0)

	chooseAddr := func() common.Address {
		return addrs[rand.Intn(len(addrs))] //nolint:gosec
	}

	chooseStorage := func() storageKey {
		return storages[rand.Intn(len(storages))] //nolint:gosec
	}

	deleteStorage := func(k storageKey) {
		storages = slices.DeleteFunc(storages, func(s storageKey) bool {
			return s == k
		})
	}

	deleteAccount := func(addr common.Address) {
		addrs = slices.DeleteFunc(addrs, func(a common.Address) bool {
			return a == addr
		})
		storages = slices.DeleteFunc(storages, func(s storageKey) bool {
			return s.addr == addr
		})
	}

	hashmemdb := rawdb.NewMemoryDatabase()
	hashdb := NewDatabaseWithConfig(hashmemdb, triedb.HashDefaults)

	firewoodmemdb := rawdb.NewMemoryDatabase()
	customrawdb.WriteDatabasePath(firewoodmemdb, tempdir)
	config := &triedb.Config{}
	fwCfg := &firewood.TrieDBConfig{
		FileName:          "coreth",
		CleanCacheSize:    1024 * 1024 * 1024,
		Revisions:         10,
		ReadCacheStrategy: ffi.CacheAllReads,
		MetricsPort:       0, // use any open port from OS
	}
	config.DBOverride = fwCfg.BackendConstructor // BackendConstructor is on the reference to allow closure
	fwdb := NewDatabaseWithConfig(firewoodmemdb, config)

	ethRoot := types.EmptyRootHash

	for i := range uint64(10_000) {
		fmt.Printf("\n\nIteration %d: %x\n", i, ethRoot)
		hashTr, err := hashdb.OpenTrie(ethRoot)
		require.NoError(t, err)
		hashSet := trienode.NewMergedNodeSet()

		fwTr, err := fwdb.OpenTrie(ethRoot)
		require.NoError(t, err)
		fwSet := trienode.NewMergedNodeSet()

		var fwKeys, fwVals [][]byte

		switch {
		case i%100 == 99: // delete acc
			addr := chooseAddr()
			accHash := hashData(addr[:])

			err = hashTr.DeleteAccount(addr)
			require.NoError(t, err)

			err = fwTr.DeleteAccount(addr)
			require.NoError(t, err)
			deleteAccount(addr)

			fwKeys = append(fwKeys, accHash[:])
			fwVals = append(fwVals, []byte{})
		case i%10 == 9: // delete storage
			storageKey := chooseStorage()
			accHash := hashData(storageKey.addr[:])
			keyHash := hashData(storageKey.key[:])

			acc, err := hashTr.GetAccount(storageKey.addr)
			require.NoError(t, err)
			hashStr, err := hashdb.OpenStorageTrie(ethRoot, storageKey.addr, acc.Root, hashTr)
			require.NoError(t, err)
			err = hashStr.DeleteStorage(storageKey.addr, storageKey.key[:])
			require.NoError(t, err)

			strRoot, set, err := hashStr.Commit(false)
			require.NoError(t, err)
			err = hashSet.Merge(set)
			require.NoError(t, err)
			acc.Root = strRoot
			err = hashTr.UpdateAccount(storageKey.addr, acc)
			require.NoError(t, err)

			acc, err = fwTr.GetAccount(storageKey.addr)
			require.NoError(t, err)
			fwStr, err := fwdb.OpenStorageTrie(ethRoot, storageKey.addr, acc.Root, fwTr)
			require.NoError(t, err)
			err = fwStr.DeleteStorage(storageKey.addr, storageKey.key[:])
			require.NoError(t, err)
			// No need to update account or commit

			deleteStorage(storageKey)
			fwKeys = append(fwKeys, append(accHash[:], keyHash[:]...))
			fwVals = append(fwVals, []byte{})

			// We must also update the account (not for hash, but to be accurate)
			fwKeys = append(fwKeys, accHash[:])
			encodedVal, err := rlp.EncodeToBytes(acc)
			require.NoError(t, err)
			fwVals = append(fwVals, encodedVal)
		case i%4 == 0: // add acc
			addr := common.BytesToAddress(hashData(binary.BigEndian.AppendUint64(nil, i)).Bytes())
			accHash := hashData(addr[:])
			acc := &types.StateAccount{
				Nonce:    1,
				Balance:  uint256.NewInt(100),
				Root:     types.EmptyRootHash,
				CodeHash: types.EmptyCodeHash[:],
			}
			enc, err := rlp.EncodeToBytes(acc)
			require.NoError(t, err)

			err = hashTr.UpdateAccount(addr, acc)
			require.NoError(t, err)

			err = fwTr.UpdateAccount(addr, acc)
			require.NoError(t, err)

			addrs = append(addrs, addr)

			fwKeys = append(fwKeys, accHash[:])
			fwVals = append(fwVals, enc)
		case i%4 == 1: // update acc
			addr := chooseAddr()
			accHash := hashData(addr[:])

			acc, err := hashTr.GetAccount(addr)
			require.NoError(t, err)
			acc.Nonce++
			enc, err := rlp.EncodeToBytes(acc)
			require.NoError(t, err)
			err = hashTr.UpdateAccount(addr, acc)
			require.NoError(t, err)

			acc, err = fwTr.GetAccount(addr)
			require.NoError(t, err)
			acc.Nonce++
			err = fwTr.UpdateAccount(addr, acc)
			require.NoError(t, err)

			fwKeys = append(fwKeys, accHash[:])
			fwVals = append(fwVals, enc)
		case i%4 == 2: // add storage
			addr := chooseAddr()
			accHash := hashData(addr[:])
			key := hashData(binary.BigEndian.AppendUint64(nil, i))
			keyHash := hashData(key[:])

			val := hashData(binary.BigEndian.AppendUint64(nil, i+1))
			storageKey := storageKey{addr: addr, key: key}

			acc, err := hashTr.GetAccount(addr)
			require.NoError(t, err)
			hashStr, err := hashdb.OpenStorageTrie(ethRoot, addr, acc.Root, hashTr)
			require.NoError(t, err)
			err = hashStr.UpdateStorage(addr, key[:], val[:])
			require.NoError(t, err)
			storages = append(storages, storageKey)

			strRoot, set, err := hashStr.Commit(false)
			require.NoError(t, err)
			err = hashSet.Merge(set)
			require.NoError(t, err)
			acc.Root = strRoot
			err = hashTr.UpdateAccount(addr, acc)
			require.NoError(t, err)

			acc, err = fwTr.GetAccount(addr)
			require.NoError(t, err)
			fwStr, err := fwdb.OpenStorageTrie(ethRoot, addr, acc.Root, fwTr)
			require.NoError(t, err)
			err = fwStr.UpdateStorage(addr, key[:], val[:])
			require.NoError(t, err)
			// no need to update account or commit

			fwKeys = append(fwKeys, append(accHash[:], keyHash[:]...))
			// UpdateStorage automatically encodes the value to rlp,
			// so we need to encode prior to sending to firewood
			encodedVal, err := rlp.EncodeToBytes(val[:])
			require.NoError(t, err)
			fwVals = append(fwVals, encodedVal)

			// We must also update the account (not for hash, but to be accurate)
			fwKeys = append(fwKeys, accHash[:])
			encodedVal, err = rlp.EncodeToBytes(acc)
			require.NoError(t, err)
			fwVals = append(fwVals, encodedVal)
		case i%4 == 3: // update storage
			storageKey := chooseStorage()
			accHash := hashData(storageKey.addr[:])
			keyHash := hashData(storageKey.key[:])

			val := hashData(binary.BigEndian.AppendUint64(nil, i+1))

			acc, err := hashTr.GetAccount(storageKey.addr)
			require.NoError(t, err)
			hashStr, err := hashdb.OpenStorageTrie(ethRoot, storageKey.addr, acc.Root, hashTr)
			require.NoError(t, err)
			err = hashStr.UpdateStorage(storageKey.addr, storageKey.key[:], val[:])
			require.NoError(t, err)

			strRoot, set, err := hashStr.Commit(false)
			require.NoError(t, err)
			err = hashSet.Merge(set)
			require.NoError(t, err)
			acc.Root = strRoot
			err = hashTr.UpdateAccount(storageKey.addr, acc)
			require.NoError(t, err)

			acc, err = fwTr.GetAccount(storageKey.addr)
			require.NoError(t, err)
			fwStr, err := fwdb.OpenStorageTrie(ethRoot, storageKey.addr, acc.Root, fwTr)
			require.NoError(t, err)
			err = fwStr.UpdateStorage(storageKey.addr, storageKey.key[:], val[:])
			require.NoError(t, err)

			fwKeys = append(fwKeys, append(accHash[:], keyHash[:]...))
			// UpdateStorage automatically encodes the value to rlp,
			// so we need to encode prior to sending to firewood
			encodedVal, err := rlp.EncodeToBytes(val[:])
			require.NoError(t, err)
			fwVals = append(fwVals, encodedVal)

			// We must also update the account (not for hash, but to be accurate)
			fwKeys = append(fwKeys, accHash[:])
			encodedVal, err = rlp.EncodeToBytes(acc)
			require.NoError(t, err)
			fwVals = append(fwVals, encodedVal)
		}

		// Update HashDB
		next, set, err := hashTr.Commit(true)
		require.NoError(t, err)
		err = hashSet.Merge(set)
		require.NoError(t, err)
		err = hashdb.TrieDB().Update(next, ethRoot, i, hashSet, nil)
		require.NoError(t, err)

		// update firewood db
		got, err := db.Update(fwKeys, fwVals)
		require.NoError(t, err)
		require.Equal(t, next[:], got)

		// Update FirewoodDB
		fwRoot, set, err := fwTr.Commit(true)
		require.NoError(t, err)
		require.Equal(t, next, fwRoot)
		err = fwSet.Merge(set)
		require.NoError(t, err)
		err = fwdb.TrieDB().Update(fwRoot, ethRoot, i, fwSet, nil)
		require.NoError(t, err)

		ethRoot = next
	}
}
