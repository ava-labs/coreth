// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pathdb

import (
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/trie/triestate"
	"github.com/ethereum/go-ethereum/common"
)

func (db *Database) UpdateAndReferenceRoot(root common.Hash, parentRoot common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	return db.Update(root, parentRoot, block, nodes, states)
}
