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
// repairAtomicTrie applies the bonus blocks to the atomic trie
func (a *atomicTrie) repairAtomicTrie(bonusBlockIDs map[uint64]ids.ID, bonusBlocks map[uint64]string) error {
	done, err := a.metadataDB.Has(repairedKey)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	root, lastCommitted := a.LastCommitted()
	tr, err := a.OpenTrie(root)
	if err != nil {
		return err
	}

	puts, removes := 0, 0
	for height, block := range bonusBlocks {
		if height > lastCommitted {
			// Avoid applying the repair to heights not yet committed
			continue
		}

		blockID, ok := bonusBlockIDs[height]
		if !ok {
			return fmt.Errorf("missing block ID for height %d", height)
		}
		txs, err := extractAtomicTxsFromRlp(block, a.codec, blockID)
		if err != nil {
			return fmt.Errorf("failed to extract atomic txs from bonus block at height %d: %w", height, err)
		}
		log.Info("repairing atomic trie", "height", height, "block", blockID, "txs", len(txs))
		if len(txs) == 0 {
			continue
		}
		combinedOps, err := mergeAtomicOps(txs)
		if err != nil {
			return err
		}
		if err := a.UpdateTrie(tr, height, combinedOps); err != nil {
			return err
		}
		for _, op := range combinedOps {
			puts += len(op.PutRequests)
			removes += len(op.RemoveRequests)
		}
		a.heightsRepaired++
	}
	newRoot, nodes := tr.Commit(false)
	if err := a.trieDB.Update(newRoot, types.EmptyRootHash, trienode.NewWithNodeSet(nodes)); err != nil {
		return err
	}
	if err := a.commit(lastCommitted, newRoot); err != nil {
		return err
	}

	if err := a.metadataDB.Put(repairedKey, []byte{1}); err != nil {
		return err
	}
	log.Info(
		"repaired atomic trie", "originalRoot", root, "newRoot", newRoot,
		"heightsRepaired", a.heightsRepaired, "puts", puts, "removes", removes,
	)
	return nil
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
