// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/coreth/core/txpool/legacypool"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ethereum/go-ethereum/common"
)

type Config config.Config

const (
	defaultAcceptorQueueLimit                     = 64 // Provides 2 minutes of buffer (2s block target) for a commit delay
	defaultPruningEnabled                         = true
	defaultCommitInterval                         = 4096
	defaultTrieCleanCache                         = 512
	defaultTrieDirtyCache                         = 512
	defaultTrieDirtyCommitTarget                  = 20
	defaultTriePrefetcherParallelism              = 16
	defaultSnapshotCache                          = 256
	defaultSyncableCommitInterval                 = defaultCommitInterval * 4
	defaultSnapshotWait                           = false
	defaultRpcGasCap                              = 50_000_000 // Default to 50M Gas Limit
	defaultRpcTxFeeCap                            = 100        // 100 AVAX
	defaultMetricsExpensiveEnabled                = true
	defaultApiMaxDuration                         = 0 // Default to no maximum API call duration
	defaultWsCpuRefillRate                        = 0 // Default to no maximum WS CPU usage
	defaultWsCpuMaxStored                         = 0 // Default to no maximum WS CPU usage
	defaultMaxBlocksPerRequest                    = 0 // Default to no maximum on the number of blocks per getLogs request
	defaultContinuousProfilerFrequency            = 15 * time.Minute
	defaultContinuousProfilerMaxFiles             = 5
	defaultPushGossipPercentStake                 = .9
	defaultPushGossipNumValidators                = 100
	defaultPushGossipNumPeers                     = 0
	defaultPushRegossipNumValidators              = 10
	defaultPushRegossipNumPeers                   = 0
	defaultPushGossipFrequency                    = 100 * time.Millisecond
	defaultPullGossipFrequency                    = 1 * time.Second
	defaultTxRegossipFrequency                    = 30 * time.Second
	defaultOfflinePruningBloomFilterSize   uint64 = 512 // Default size (MB) for the offline pruner to use
	defaultLogLevel                               = "info"
	defaultLogJSONFormat                          = false
	defaultMaxOutboundActiveRequests              = 16
	defaultPopulateMissingTriesParallelism        = 1024
	defaultStateSyncServerTrieCache               = 64 // MB
	defaultAcceptedCacheSize                      = 32 // blocks

	// defaultStateSyncMinBlocks is the minimum number of blocks the blockchain
	// should be ahead of local last accepted to perform state sync.
	// This constant is chosen so normal bootstrapping is preferred when it would
	// be faster than state sync.
	// time assumptions:
	// - normal bootstrap processing time: ~14 blocks / second
	// - state sync time: ~6 hrs.
	defaultStateSyncMinBlocks   = 300_000
	DefaultStateSyncRequestSize = 1024 // the number of key/values to ask peers for per request
)

var (
	defaultEnabledAPIs = []string{
		"eth",
		"eth-filter",
		"net",
		"web3",
		"internal-eth",
		"internal-blockchain",
		"internal-transaction",
	}
	defaultAllowUnprotectedTxHashes = []common.Hash{
		common.HexToHash("0xfefb2da535e927b85fe68eb81cb2e4a5827c905f78381a01ef2322aa9b0aee8e"), // EIP-1820: https://eips.ethereum.org/EIPS/eip-1820
	}
)

// EthAPIs returns an array of strings representing the Eth APIs that should be enabled
func (c Config) EthAPIs() []string {
	return c.EnabledEthAPIs
}

func (c Config) EthBackendSettings() eth.Settings {
	return eth.Settings{MaxBlocksPerRequest: c.MaxBlocksPerRequest}
}

func (c *Config) SetDefaults() {
	c.EnabledEthAPIs = defaultEnabledAPIs
	c.RPCGasCap = defaultRpcGasCap
	c.RPCTxFeeCap = defaultRpcTxFeeCap
	c.MetricsExpensiveEnabled = defaultMetricsExpensiveEnabled

	c.TxPoolPriceLimit = legacypool.DefaultConfig.PriceLimit
	c.TxPoolPriceBump = legacypool.DefaultConfig.PriceBump
	c.TxPoolAccountSlots = legacypool.DefaultConfig.AccountSlots
	c.TxPoolGlobalSlots = legacypool.DefaultConfig.GlobalSlots
	c.TxPoolAccountQueue = legacypool.DefaultConfig.AccountQueue
	c.TxPoolGlobalQueue = legacypool.DefaultConfig.GlobalQueue
	c.TxPoolLifetime.Duration = legacypool.DefaultConfig.Lifetime

	c.APIMaxDuration.Duration = defaultApiMaxDuration
	c.WSCPURefillRate.Duration = defaultWsCpuRefillRate
	c.WSCPUMaxStored.Duration = defaultWsCpuMaxStored
	c.MaxBlocksPerRequest = defaultMaxBlocksPerRequest
	c.ContinuousProfilerFrequency.Duration = defaultContinuousProfilerFrequency
	c.ContinuousProfilerMaxFiles = defaultContinuousProfilerMaxFiles
	c.Pruning = defaultPruningEnabled
	c.TrieCleanCache = defaultTrieCleanCache
	c.TrieDirtyCache = defaultTrieDirtyCache
	c.TrieDirtyCommitTarget = defaultTrieDirtyCommitTarget
	c.TriePrefetcherParallelism = defaultTriePrefetcherParallelism
	c.SnapshotCache = defaultSnapshotCache
	c.AcceptorQueueLimit = defaultAcceptorQueueLimit
	c.CommitInterval = defaultCommitInterval
	c.SnapshotWait = defaultSnapshotWait
	c.PushGossipPercentStake = defaultPushGossipPercentStake
	c.PushGossipNumValidators = defaultPushGossipNumValidators
	c.PushGossipNumPeers = defaultPushGossipNumPeers
	c.PushRegossipNumValidators = defaultPushRegossipNumValidators
	c.PushRegossipNumPeers = defaultPushRegossipNumPeers
	c.PushGossipFrequency.Duration = defaultPushGossipFrequency
	c.PullGossipFrequency.Duration = defaultPullGossipFrequency
	c.RegossipFrequency.Duration = defaultTxRegossipFrequency
	c.OfflinePruningBloomFilterSize = defaultOfflinePruningBloomFilterSize
	c.LogLevel = defaultLogLevel
	c.LogJSONFormat = defaultLogJSONFormat
	c.MaxOutboundActiveRequests = defaultMaxOutboundActiveRequests
	c.PopulateMissingTriesParallelism = defaultPopulateMissingTriesParallelism
	c.StateSyncServerTrieCache = defaultStateSyncServerTrieCache
	c.StateSyncCommitInterval = defaultSyncableCommitInterval
	c.StateSyncMinBlocks = defaultStateSyncMinBlocks
	c.StateSyncRequestSize = DefaultStateSyncRequestSize
	c.AllowUnprotectedTxHashes = defaultAllowUnprotectedTxHashes
	c.AcceptedCacheSize = defaultAcceptedCacheSize
}

// Validate returns an error if this is an invalid config.
func (c *Config) Validate(networkID uint32) error {
	// Ensure that non-standard commit interval is not allowed for production networks
	if constants.ProductionNetworkIDs.Contains(networkID) {
		if c.CommitInterval != defaultCommitInterval {
			return fmt.Errorf("cannot start non-local network with commit interval %d different than %d", c.CommitInterval, defaultCommitInterval)
		}
		if c.StateSyncCommitInterval != defaultSyncableCommitInterval {
			return fmt.Errorf("cannot start non-local network with syncable interval %d different than %d", c.StateSyncCommitInterval, defaultSyncableCommitInterval)
		}
	}

	if c.PopulateMissingTries != nil && (c.OfflinePruning || c.Pruning) {
		return fmt.Errorf("cannot enable populate missing tries while offline pruning (enabled: %t)/pruning (enabled: %t) are enabled", c.OfflinePruning, c.Pruning)
	}
	if c.PopulateMissingTries != nil && c.PopulateMissingTriesParallelism < 1 {
		return fmt.Errorf("cannot enable populate missing tries without at least one reader (parallelism: %d)", c.PopulateMissingTriesParallelism)
	}

	if !c.Pruning && c.OfflinePruning {
		return fmt.Errorf("cannot run offline pruning while pruning is disabled")
	}
	// If pruning is enabled, the commit interval must be non-zero so the node commits state tries every CommitInterval blocks.
	if c.Pruning && c.CommitInterval == 0 {
		return fmt.Errorf("cannot use commit interval of 0 with pruning enabled")
	}

	if c.PushGossipPercentStake < 0 || c.PushGossipPercentStake > 1 {
		return fmt.Errorf("push-gossip-percent-stake is %f but must be in the range [0, 1]", c.PushGossipPercentStake)
	}
	return nil
}

func (c *Config) Deprecate() string {
	return (*config.Config)(c).Deprecate()
}
