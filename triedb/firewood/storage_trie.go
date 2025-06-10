// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie/trienode"
)

type StorageTrie struct {
	*AccountTrie
	storageRoot common.Hash
}

// NewStorageTrie creates a wrapper storage trie for the given account.
// The account trie handles all operations besides hashing.
func NewStorageTrie(accountTrie *AccountTrie, storageRoot common.Hash) (*StorageTrie, error) {
	return &StorageTrie{
		AccountTrie: accountTrie,
		storageRoot: storageRoot,
	}, nil
}

// Actual commit is handled by the account trie.
// Return the old storage root in case there was no change.
func (s *StorageTrie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	return s.storageRoot, nil, nil
}

// Hash implements state.Trie.
func (s *StorageTrie) Hash() common.Hash {
	return s.storageRoot // only used in statedb, we work around it.
}
