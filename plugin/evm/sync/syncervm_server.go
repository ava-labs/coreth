// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/core"
	"github.com/ethereum/go-ethereum/log"
)

type SummaryProvider interface {
	StateSummaryAtHeight(height uint64) (block.StateSummary, error)
}

type stateSyncServer struct {
	chain    *core.BlockChain
	provider SummaryProvider

	syncableInterval uint64
}

type StateSyncServer interface {
	GetLastStateSummary(context.Context) (block.StateSummary, error)
	GetStateSummary(context.Context, uint64) (block.StateSummary, error)
}

func NewStateSyncServer(chain *core.BlockChain, provider SummaryProvider, syncableInterval uint64) StateSyncServer {
	return &stateSyncServer{
		chain:            chain,
		provider:         provider,
		syncableInterval: syncableInterval,
	}
}

// GetLastStateSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by [syncableInterval]
// If no summary is available, [database.ErrNotFound] must be returned.
func (server *stateSyncServer) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	lastHeight := server.chain.LastAcceptedBlock().NumberU64()
	lastSyncSummaryNumber := lastHeight - lastHeight%server.syncableInterval

	summary, err := server.provider.StateSummaryAtHeight(lastSyncSummaryNumber)
	if err != nil {
		log.Debug("could not get latest state summary", "err", err)
		return nil, database.ErrNotFound
	}
	log.Debug("Serving syncable block at latest height", "summary", summary)
	return summary, nil
}

// GetStateSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [height] if the node can serve state sync data for that key.
// If not, [database.ErrNotFound] must be returned.
func (server *stateSyncServer) GetStateSummary(_ context.Context, height uint64) (block.StateSummary, error) {
	summaryBlock := server.chain.GetBlockByNumber(height)
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > server.chain.LastAcceptedBlock().NumberU64() ||
		summaryBlock.NumberU64()%server.syncableInterval != 0 {
		return nil, database.ErrNotFound
	}

	summary, err := server.provider.StateSummaryAtHeight(summaryBlock.NumberU64())
	if err != nil {
		log.Debug("could not get state summary", "height", height, "err", err)
		return nil, database.ErrNotFound
	}

	log.Debug("Serving syncable block at requested height", "height", height, "summary", summary)
	return summary, nil
}
