// (c) 2025, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb/database"
)

var (
	errNodeNotFound = errors.New("node not found")
	errNoReader     = errors.New("reader unavailable")
)

// AccountTrie implements state.Trie for managing account states.
// There are several caveats to the current implementation:
//  1. It is not thread-safe.
//  2. After making any Update/Delete operations, no calls to `GetAccount` or `GetStorage`
//     should be made. This is not yet guaranteed.
//  3. `Commit` is not used as expected in the state package. Instead, it updates firewood's
//     internal state to track the proposal
//  4. The `Hash` method actually creates the proposal, since Firewood cannot calculate
//     the hash of the trie without committing it. No changes can be made after this.
type AccountTrie struct {
	fw           *Database
	parentRoot   common.Hash
	root         common.Hash
	reader       database.Reader
	updateLock   sync.RWMutex
	updateKeys   [][]byte
	updateValues [][]byte
}

func NewAccountTrie(root common.Hash, db *Database) (*AccountTrie, error) {
	reader, err := db.Reader(root)
	if err != nil {
		return nil, errors.Join(errNoReader, err)
	}
	return &AccountTrie{
		fw:         db,
		parentRoot: root,
		reader:     reader,
	}, nil
}

// GetAccount implements state.Trie.
func (a *AccountTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
	a.updateLock.RLock()
	defer a.updateLock.RUnlock()

	if a.reader == nil {
		return nil, errNoReader
	}

	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()

	acctBytes, err := a.reader.Node(common.Hash{}, key, common.Hash{})
	if err != nil {
		return nil, err
	}

	if acctBytes == nil {
		return nil, errNodeNotFound
	}

	// Decode the account node
	acct := new(types.StateAccount)
	err = rlp.DecodeBytes(acctBytes, acct)
	return acct, err
}

// GetStorage implements state.Trie.
func (a *AccountTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
	a.updateLock.RLock()
	defer a.updateLock.RUnlock()

	if a.reader == nil {
		return nil, errNoReader
	}

	acctKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	key = append(acctKey, storageKey...)
	storageBytes, err := a.reader.Node(common.Hash{}, key, common.Hash{})
	if err != nil {
		return nil, err
	}
	if storageBytes == nil {
		return nil, errNodeNotFound
	}

	return storageBytes, nil
}

// UpdateAccount implements state.Trie.
func (a *AccountTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	a.updateLock.Lock()
	defer a.updateLock.Unlock()
	if a.reader == nil {
		return errNoReader
	}

	// Queue the keys and values for later commit
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		return err
	}
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, data)
	return nil
}

// UpdateStorage implements state.Trie.
func (a *AccountTrie) UpdateStorage(addr common.Address, key []byte, value []byte) error {
	a.updateLock.Lock()
	defer a.updateLock.Unlock()
	if a.reader == nil {
		return errNoReader
	}

	acctKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	key = append(acctKey, storageKey...)
	data, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}
	// Queue the keys and values for later commit
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, data)
	return nil
}

// DeleteAccount implements state.Trie.
func (a *AccountTrie) DeleteAccount(addr common.Address) error {
	a.updateLock.Lock()
	defer a.updateLock.Unlock()
	if a.reader == nil {
		return errNoReader
	}

	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	// Queue the key for deletion
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, []byte{}) // nil value indicates deletion
	return nil
}

// DeleteStorage implements state.Trie.
func (a *AccountTrie) DeleteStorage(addr common.Address, key []byte) error {
	a.updateLock.Lock()
	defer a.updateLock.Unlock()
	if a.reader == nil {
		return errNoReader
	}

	acctKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	key = append(acctKey, storageKey...)
	// Queue the key for deletion
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, []byte{}) // nil value indicates deletion
	return nil
}

// Hash implements state.Trie.
func (a *AccountTrie) Hash() common.Hash {
	a.updateLock.Lock()
	defer a.updateLock.Unlock()
	hash, err := a.hash()
	if err != nil {
		log.Error("Failed to hash account trie", "error", err)
	}
	return hash
}

func (a *AccountTrie) hash() (common.Hash, error) {
	// If we haven't already hashed, we need to do so.
	if a.root != (common.Hash{}) {
		return a.root, nil
	}
	return a.fw.getProposalHash(a.parentRoot, a.updateKeys, a.updateValues)
}

// Commit implements state.Trie.
func (a *AccountTrie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	a.updateLock.Lock()
	defer a.updateLock.Unlock()

	// Get the hash of the trie.
	hash, err := a.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}

	// Create the NodeSet. This will be sent to `Update` later.
	nodeset := trienode.NewNodeSet(a.parentRoot)
	for i, key := range a.updateKeys {
		value := a.updateValues[i]
		fmt.Printf("Adding node %x: %x\n", key, value)
		nodeset.AddNode(key, &trienode.Node{
			Blob: value,
		})
	}
	return hash, nodeset, nil
}

// UpdateContractCode implements state.Trie.
// Contract code is controlled by rawdb, so we don't need to do anything here.
func (a *AccountTrie) UpdateContractCode(_ common.Address, _ common.Hash, _ []byte) error {
	return nil
}

// GetKey implements state.Trie.
func (a *AccountTrie) GetKey(_ []byte) []byte {
	panic("unimplemented")
}

// NodeIterator implements state.Trie.
func (a *AccountTrie) NodeIterator(_ []byte) (trie.NodeIterator, error) {
	panic("unimplemented")
}

// Prove implements state.Trie.
func (a *AccountTrie) Prove(_ []byte, _ ethdb.KeyValueWriter) error {
	panic("unimplemented")
}
