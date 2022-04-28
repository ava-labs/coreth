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

type stateSyncServerConfig struct {
	NetCodec   codec.Manager
	Chain      *core.BlockChain
	AtomicTrie AtomicTrie

	// SyncableInterval is the interval at which blocks are eligible to provide syncable block summaries.
	SyncableInterval uint64
}

type stateSyncServer struct {
	chain      *core.BlockChain
	atomicTrie AtomicTrie

	netCodec codec.Manager

	syncableInterval uint64
}

type StateSyncServer interface {
	GetLastStateSummary() (commonEng.Summary, error)
	GetStateSummary(uint64) (commonEng.Summary, error)
}

func NewStateSyncServer(config *stateSyncServerConfig) StateSyncServer {
	return &stateSyncServer{
		chain:            config.Chain,
		atomicTrie:       config.AtomicTrie,
		netCodec:         config.NetCodec,
		syncableInterval: config.SyncableInterval,
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

// GetLastStateSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by [syncableInterval]
func (server *stateSyncServer) GetLastStateSummary() (commonEng.Summary, error) {
	lastHeight := server.chain.LastAcceptedBlock().NumberU64()
	lastSyncSummaryNumber := lastHeight - lastHeight%server.syncableInterval

	return server.stateSummaryAtHeight(lastSyncSummaryNumber)
}

// GetStateSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [key] if the node can serve state sync data for that key.
func (server *stateSyncServer) GetStateSummary(key uint64) (commonEng.Summary, error) {
	summaryBlock := server.chain.GetBlockByNumber(key)
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > server.chain.LastAcceptedBlock().NumberU64() ||
		summaryBlock.NumberU64()%server.syncableInterval != 0 {
		return nil, commonEng.ErrUnknownStateSummary
	}

	return server.stateSummaryAtHeight(summaryBlock.NumberU64())
}
