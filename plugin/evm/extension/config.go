package extension

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

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

var (
	errNilConfig              = errors.New("nil config")
	errNilNetworkCodec        = errors.New("nil network codec")
	errNilSyncSummaryProvider = errors.New("nil sync summary provider")
	errNilSyncableParser      = errors.New("nil syncable parser")
)

type ExtensibleVM interface {
	// SetExtensionConfig sets the configuration for the VM extension
	// Should be called before any other method and only once
	SetExtensionConfig(config *Config) error

	// NewClient returns a client to send messages with for the given protocol
	NewClient(protocol uint64, options ...p2p.ClientOption) *p2p.Client
	// AddHandler registers a server handler for an application protocol
	AddHandler(protocol uint64, handler p2p.Handler) error
	// SetLastAcceptedBlock sets the last accepted block
	SetLastAcceptedBlock(lastAcceptedBlock snowman.Block) error
	// GetVMBlock returns the VMBlock for the given ID or an error if the block is not found
	GetVMBlock(context.Context, ids.ID) (VMBlock, error)
	// NewVMBlock returns a new VMBlock for the given Eth block
	NewVMBlock(*types.Block) (VMBlock, error)
	// LastAcceptedVMBlock returns the last accepted VM block
	LastAcceptedVMBlock() VMBlock
	// IsBootstrapped returns true if the VM is bootstrapped
	IsBootstrapped() bool

	// Validators returns the validators for the network
	Validators() *p2p.Validators
	// Ethereum returns the Ethereum client
	Ethereum() *eth.Ethereum
	// Config returns the configuration for the VM
	Config() config.Config
	// MetricRegistry returns the metric registry for the VM
	MetricRegistry() *prometheus.Registry
	// ReadLastAccepted returns the last accepted block hash and height
	ReadLastAccepted() (common.Hash, uint64, error)
	// CurrentRules returns the current rules for the VM
	CurrentRules() params.Rules
	// VersionDB returns the versioned database for the VM
	VersionDB() *versiondb.Database
	// SyncerClient returns the syncer client for the VM
	SyncerClient() sync.Client
}

// InnerVM is the interface that must be implemented by the VM
// that's being wrapped by the extension
type InnerVM interface {
	ExtensibleVM
	avalanchecommon.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

// VMBlock is a block that can be used by the extension
type VMBlock interface {
	snowman.Block
	GetEthBlock() *types.Block
}

// BlockManagerExtension is an extension for the block manager
// to handle BlockManager events
type BlockManagerExtension interface {
	// SemanticVerify verifies the block semantically
	// it can be implemented to extend inner block verification
	SemanticVerify(b VMBlock) error
	// SyntacticVerify verifies the block syntactically
	// it can be implemented to extend inner block verification
	SyntacticVerify(b VMBlock, rules params.Rules) error
	// OnAccept is called when a block is accepted by the block manager
	OnAccept(b VMBlock, acceptedBatch database.Batch) error
	// OnReject is called when a block is rejected by the block manager
	OnReject(b VMBlock) error
	// OnCleanup is called when a block cleanup is requested from the block manager
	OnCleanup(b VMBlock)
}

// BuilderMempool is a mempool that's used in the block builder
type BuilderMempool interface {
	// PendingLen returns the number of pending transactions
	// that are waiting to be included in a block
	PendingLen() int
	// SubscribePendingTxs returns a channel that's signaled when there are pending transactions
	SubscribePendingTxs() <-chan struct{}
}

// LeafRequestConfig is the configuration to handle leaf requests
// in the network and syncer
type LeafRequestConfig struct {
	// LeafType is the type of the leaf node
	LeafType message.NodeType
	// MetricName is the name of the metric to use for the leaf request
	MetricName string
	// Handler is the handler to use for the leaf request
	Handler handlers.LeafRequestHandler
}

// Config is the configuration for the VM extension
type Config struct {
	// NetworkCodec is the codec manager to use
	// for encoding and decoding network messages.
	// It's required and should be non-nil
	NetworkCodec codec.Manager
	// ConsensusCallbacks is the consensus callbacks to use
	// for the VM to be used in consensus engine.
	// Callback functions can be nil.
	ConsensusCallbacks dummy.ConsensusCallbacks
	// SyncSummaryProvider is the sync summary provider to use
	// for the VM to be used in syncer.
	// It's required and should be non-nil
	SyncSummaryProvider sync.SummaryProvider
	// SyncExtender can extend the syncer to handle custom sync logic.
	// It's optional and can be nil
	SyncExtender sync.Extender
	// SyncableParser is to parse summary messages from the network.
	// It's required and should be non-nil
	SyncableParser message.SyncableParser
	// BlockManagerExtension is the extension for the block manager
	// to handle block manager events.
	// It's optional and can be nil
	BlockExtension BlockManagerExtension
	// ExtraSyncLeafConfig is the extra configuration to handle leaf requests
	// in the network and syncer. It's optional and can be nil
	ExtraSyncLeafConfig *LeafRequestConfig
	// ExtraMempool is the mempool to be used in the block builder.
	// It's optional and can be nil
	ExtraMempool BuilderMempool
	// Clock is the clock to use for time related operations.
	// It's optional and can be nil
	Clock *mockable.Clock
}

func (c *Config) Validate() error {
	if c == nil {
		return errNilConfig
	}
	if c.NetworkCodec == nil {
		return errNilNetworkCodec
	}
	if c.SyncSummaryProvider == nil {
		return errNilSyncSummaryProvider
	}
	if c.SyncableParser == nil {
		return errNilSyncableParser
	}
	return nil
}
