// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/coreth/utils"
)

func getDefaultConfig() Config {
	return Config{
		// Provides 2 minutes of buffer (2s block target) for a commit delay
		AcceptorQueueLimit:        64,
		Pruning:                   true,
		CommitInterval:            4096,
		TrieCleanCache:            512,
		TrieDirtyCache:            512,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 16,
		SnapshotCache:             256,
		StateSyncCommitInterval:   DefaultCommitInterval * 4,
		SnapshotWait:              false,
		RPCGasCap:                 50_000_000, // 50M Gas Limit
		RPCTxFeeCap:               100,        // 100 AVAX
		MetricsExpensiveEnabled:   true,
		// Default to no maximum API call duration
		APIMaxDuration: timeToDuration(0),
		// Default to no maximum WS CPU usage
		WSCPURefillRate: timeToDuration(0),
		// Default to no maximum WS CPU usage
		WSCPUMaxStored: timeToDuration(0),
		// Default to no maximum on the number of blocks per getLogs request
		MaxBlocksPerRequest:         0,
		ContinuousProfilerFrequency: timeToDuration(15 * time.Minute),
		ContinuousProfilerMaxFiles:  5,
		PushGossipPercentStake:      .9,
		PushGossipNumValidators:     100,
		PushGossipNumPeers:          0,
		PushRegossipNumValidators:   10,
		PushRegossipNumPeers:        0,
		PushGossipFrequency:         timeToDuration(100 * time.Millisecond),
		PullGossipFrequency:         timeToDuration(1 * time.Second),
		RegossipFrequency:           timeToDuration(30 * time.Second),
		// Default size (MB) for the offline pruner to use
		OfflinePruningBloomFilterSize:   uint64(512),
		LogLevel:                        "info",
		LogJSONFormat:                   false,
		MaxOutboundActiveRequests:       16,
		PopulateMissingTriesParallelism: 1024,
		StateSyncServerTrieCache:        64, // MB
		AcceptedCacheSize:               32, // blocks
		// StateSyncMinBlocks is the minimum number of blocks the blockchain
		// should be ahead of local last accepted to perform state sync.
		// This constant is chosen so normal bootstrapping is preferred when it would
		// be faster than state sync.
		// time assumptions:
		// - normal bootstrap processing time: ~14 blocks / second
		// - state sync time: ~6 hrs.
		StateSyncMinBlocks: 300_000,
		// the number of key/values to ask peers for per request
		StateSyncRequestSize: 1024,
		// Estimated block count in 24 hours with 2s block accept period
		HistoricalProofQueryWindow: uint64(24 * time.Hour / (2 * time.Second)),
		// Price Option Defaults
		PriceOptionSlowFeePercentage: uint64(95),
		PriceOptionFastFeePercentage: uint64(105),
		PriceOptionMaxTip:            uint64(20 * utils.GWei),
		// Mempool settings
		TxPoolLifetime: timeToDuration(3 * time.Hour),
	}
}

func timeToDuration(t time.Duration) Duration {
	return Duration{t}
}
