package extension

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ethereum/go-ethereum/common"
)

// TODO: move this file out from atomic pkg

type ExtensibleVM interface {
	// NewClient returns a client to send messages with for the given protocol
	NewClient(protocol uint64, options ...p2p.ClientOption) *p2p.Client
	// AddHandler registers a server handler for an application protocol
	AddHandler(protocol uint64, handler p2p.Handler) error
	GetBlockExtended(ctx context.Context, blkID ids.ID) (ExtendedBlock, error)
	LastAcceptedBlockInternal() snowman.Block
	Validators() *p2p.Validators
	SetExtensionConfig(config *Config) error
	Blockchain() *core.BlockChain
	Config() *config.Config
	MetricRegistry() *prometheus.Registry
	ReadLastAccepted() (common.Hash, uint64, error)
	VersionDB() *versiondb.Database
}

type InnerVM interface {
	ExtensibleVM
	avalanchecommon.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

type ExtendedBlock interface {
	snowman.Block
	GetExtraData() interface{}
	GetEthBlock() *types.Block
}

type BlockExtension interface {
	InitializeExtraData(ethBlock *types.Block, chainConfig *params.ChainConfig) (any, error)
	SyntacticVerify(b ExtendedBlock, rules params.Rules) error
	Accept(b ExtendedBlock, acceptedBatch database.Batch) error
	Reject(b ExtendedBlock) error
	Cleanup(b ExtendedBlock)
}

type BuilderMempool interface {
	Len() int
	SubscribePendingTxs() <-chan struct{}
}

type LeafRequestConfig struct {
	LeafType   message.NodeType
	MetricName string
	Handler    handlers.LeafRequestHandler
}

type Config struct {
	NetworkCodec        codec.Manager
	ConsensusCallbacks  *dummy.ConsensusCallbacks
	SyncSummaryProvider sync.SummaryProvider
	SyncExtender        sync.Extender
	SyncableParser      message.SyncableParser
	BlockExtension      BlockExtension
	SyncLeafType        *LeafRequestConfig
	ExtraMempool        BuilderMempool
}
