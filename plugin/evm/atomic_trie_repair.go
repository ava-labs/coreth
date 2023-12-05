// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	repairedKey = []byte("atomicTrieHasBonusBlocks")
)

// TODO: Remove this after the DUpgrade
// repairAtomicTrie applies the bonus blocks to the atomic trie so all nodes
// can have a canonical atomic trie.
// Initially, bonus blocks were not indexed into the atomic trie. However, a
// regression caused some nodes to index these blocks.
// Returns the number of heights repaired.
func (a *atomicTrie) repairAtomicTrie(bonusBlockIDs map[uint64]ids.ID, bonusBlocks map[uint64]string) (int, error) {
	done, err := a.metadataDB.Has(repairedKey)
	if err != nil {
		return 0, err
	}
	if done {
		return 0, nil
	}

	root, lastCommitted := a.LastCommitted()
	tr, err := a.OpenTrie(root)
	if err != nil {
		return 0, err
	}

	heightsRepaired := 0
	puts, removes := 0, 0
	for height, block := range bonusBlocks {
		if height > lastCommitted {
			// Avoid applying the repair to heights not yet committed
			continue
		}

		blockID, ok := bonusBlockIDs[height]
		if !ok {
			return 0, fmt.Errorf("missing block ID for height %d", height)
		}
		txs, err := extractAtomicTxsFromRlp(block, a.codec, blockID)
		if err != nil {
			return 0, fmt.Errorf("failed to extract atomic txs from bonus block at height %d: %w", height, err)
		}
		log.Info("repairing atomic trie", "height", height, "block", blockID, "txs", len(txs))
		if len(txs) == 0 {
			continue // Should not happen
		}
		combinedOps, err := mergeAtomicOps(txs)
		if err != nil {
			return 0, err
		}
		if err := a.UpdateTrie(tr, height, combinedOps); err != nil {
			return 0, err
		}
		for _, op := range combinedOps {
			puts += len(op.PutRequests)
			removes += len(op.RemoveRequests)
		}
		heightsRepaired++
	}
	newRoot, nodes := tr.Commit(false)
	if err := a.trieDB.Update(newRoot, types.EmptyRootHash, trienode.NewWithNodeSet(nodes)); err != nil {
		return 0, err
	}
	if err := a.commit(lastCommitted, newRoot); err != nil {
		return 0, err
	}

	if err := a.metadataDB.Put(repairedKey, []byte{1}); err != nil {
		return 0, err
	}
	log.Info(
		"repaired atomic trie", "originalRoot", root, "newRoot", newRoot,
		"heightsRepaired", heightsRepaired, "puts", puts, "removes", removes,
	)
	return heightsRepaired, nil
}

func extractAtomicTxsFromRlp(rlpHex string, codec codec.Manager, expectedHash ids.ID) ([]*Tx, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(common.Hex2Bytes(rlpHex), ethBlock); err != nil {
		return nil, err
	}
	if ids.ID(ethBlock.Hash()) != expectedHash {
		return nil, fmt.Errorf("block ID mismatch at (%s != %s)", ids.ID(ethBlock.Hash()), expectedHash)
	}
	return ExtractAtomicTxs(ethBlock.ExtData(), false, codec)
}

func (a *atomicBackend) Repair(bonusBlocksRlp map[uint64]string) error {
	if len(bonusBlocksRlp) > 0 {
		if a.stateSynced {
			// If the node is state synced, then the atomic trie may be incorrect
			// so let's insert the bonus blocks into the atomic trie.
			if err := a.atomicTrie.metadataDB.Delete(repairedKey); err != nil {
				return err
			}
		}
		if _, err := a.atomicTrie.repairAtomicTrie(a.bonusBlocks, bonusBlocksRlp); err != nil {
			return err
		}
		if err := a.db.Commit(); err != nil {
			return err
		}
	}
	return nil
}
