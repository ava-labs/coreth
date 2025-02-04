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
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ava-labs/coreth/sync/handlers"
)

type ExtensibleVM interface {
	SetLastAcceptedBlock(lastAcceptedBlock snowman.Block) error
	GetVMBlock(context.Context, ids.ID) (VMBlock, error)
	NewVMBlock(*types.Block) (VMBlock, error)
	LastAcceptedVMBlock() VMBlock
	// NewClient returns a client to send messages with for the given protocol
	NewClient(protocol uint64, options ...p2p.ClientOption) *p2p.Client
	// AddHandler registers a server handler for an application protocol
	AddHandler(protocol uint64, handler p2p.Handler) error
	LastAcceptedBlockInternal() snowman.Block
	Validators() *p2p.Validators
	SetExtensionConfig(config *Config) error
	Ethereum() *eth.Ethereum
	Config() *config.Config
	MetricRegistry() *prometheus.Registry
	ReadLastAccepted() (common.Hash, uint64, error)
	CurrentRules() params.Rules
	VersionDB() *versiondb.Database
	SyncerClient() sync.Client
}

type InnerVM interface {
	ExtensibleVM
	avalanchecommon.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

type VMBlock interface {
	snowman.Block
	GetEthBlock() *types.Block
}

type BlockManagerExtension interface {
	SyntacticVerify(b VMBlock, rules params.Rules) error
	Accept(b VMBlock, acceptedBatch database.Batch) error
	Reject(b VMBlock) error
	Cleanup(b VMBlock)
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
	ConsensusCallbacks  dummy.ConsensusCallbacks
	SyncSummaryProvider sync.SummaryProvider
	SyncExtender        sync.Extender
	SyncableParser      message.SyncableParser
	BlockExtension      BlockManagerExtension
	SyncLeafType        *LeafRequestConfig
	ExtraMempool        BuilderMempool
}
