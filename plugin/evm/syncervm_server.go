// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// defaultSyncableInterval is chosen to balance the number of historical
	// trie roots nodes participating in the state sync protocol need to keep,
	// and the time nodes joining the network have to complete the sync.
	// Higher values also add to the worst case number of blocks new nodes
	// joining the network must process after state sync.
	// defaultSyncableInterval must be a multiple of [core.CommitInterval]
	// time assumptions:
	// - block issuance time ~2s (time per 4096 blocks ~2hrs).
	// - state sync time: ~6 hrs. (assuming even slow nodes will complete in ~12hrs)
	// - normal bootstrap processing time: ~14 blocks / second
	// 4 * 4096 allows a sync time of up to 16 hrs and keeping two historical
	// trie roots (in addition to the current state trie), while adding a
	// worst case overhead of ~15 mins in form of post-sync block processing
	// time for new nodes.
	defaultSyncableInterval = uint64(4 * core.CommitInterval)
)

type stateSyncServer struct {
	chain      *core.BlockChain
	atomicTrie AtomicTrie

	netCodec codec.Manager

	syncableInterval uint64
}

type StateSyncServer interface {
	StateSyncGetLastSummary() (commonEng.Summary, error)
	StateSyncGetSummary(uint64) (commonEng.Summary, error)
}

func NewStateSyncServer(chain *core.BlockChain, atomicTrie AtomicTrie, netCodec codec.Manager) StateSyncServer {
	return &stateSyncServer{
		chain:            chain,
		atomicTrie:       atomicTrie,
		netCodec:         netCodec,
		syncableInterval: defaultSyncableInterval,
	}
}

// stateSummaryAtHeight returns the SyncSummary at [height] if valid and available.
func (server *stateSyncServer) stateSummaryAtHeight(height uint64) (message.SyncSummary, error) {
	atomicRoot, err := server.atomicTrie.Root(height)
	if err != nil {
		return message.SyncSummary{}, fmt.Errorf("error getting atomic trie root for height (%d): %w", height, err)
	}

	if (atomicRoot == common.Hash{}) {
		return message.SyncSummary{}, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	blk := server.chain.GetBlockByNumber(height)
	if blk == nil {
		return message.SyncSummary{}, fmt.Errorf("block not found for height (%d)", height)
	}

	if !server.chain.HasState(blk.Root()) {
		return message.SyncSummary{}, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}

	summary, err := message.NewSyncSummary(server.netCodec, blk.Hash(), height, blk.Root(), atomicRoot)
	if err != nil {
		return message.SyncSummary{}, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}

	log.Debug("Serving syncable block", "height", height, "summary", summary)
	return summary, nil
}

// StateSyncGetLastSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by [syncableInterval]
func (server *stateSyncServer) StateSyncGetLastSummary() (commonEng.Summary, error) {
	lastHeight := server.chain.LastAcceptedBlock().NumberU64()
	lastSyncSummaryNumber := lastHeight - lastHeight%server.syncableInterval

	return server.stateSummaryAtHeight(lastSyncSummaryNumber)
}

// StateSyncGetSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [key] if the node can serve state sync data for that key.
func (server *stateSyncServer) StateSyncGetSummary(key uint64) (commonEng.Summary, error) {
	summaryBlock := server.chain.GetBlockByNumber(key)
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > server.chain.LastAcceptedBlock().NumberU64() ||
		summaryBlock.NumberU64()%server.syncableInterval != 0 {
		return nil, commonEng.ErrUnknownStateSummary
	}

	return server.stateSummaryAtHeight(summaryBlock.NumberU64())
}
