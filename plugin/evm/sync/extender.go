package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
)

var _ Extender = (*NoOpExtender)(nil)

type Extender interface {
	Sync(ctx context.Context, client syncclient.LeafClient, verdb *versiondb.Database, syncSummary message.Syncable) error
	OnFinishBeforeCommit(lastAcceptedHeight uint64, syncSummary message.Syncable) error
	OnFinishAfterCommit(summaryHeight uint64) error
}

type NoOpExtender struct{}

func (n *NoOpExtender) Sync(ctx context.Context, client syncclient.LeafClient, verdb *versiondb.Database, syncSummary message.Syncable) error {
	return nil
}

func (n *NoOpExtender) OnFinishBeforeCommit(lastAcceptedHeight uint64, syncSummary message.Syncable) error {
	return nil
}

func (n *NoOpExtender) OnFinishAfterCommit(summaryHeight uint64) error {
	return nil
}
