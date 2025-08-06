# Coreth Configuration

> **Note**: These are the configuration options available in the coreth codebase. To set these values, you need to create a configuration file at `{chain-config-dir}/C/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/C/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes all configuration options available for coreth.

The C-Chain config is printed out in the log when a node starts. Default values for each config flag are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

## Configuration Format

Configuration is provided as a JSON object. All fields are optional unless otherwise specified.

## API Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `rpc-gas-cap` | integer | The maximum gas to be consumed by an RPC Call (used in `eth_estimateGas` and `eth_call`). | `50000000` |
| `rpc-tx-fee-cap` | integer | Global transaction fee (price \* `gaslimit`) cap (measured in AVAX) for send-transaction variants. | `100` |
| `api-max-duration` | duration | Maximum API call duration. If API calls exceed this duration, they will time out. | `0` (no maximum) |
| `api-max-blocks-per-request` | integer | Maximum number of blocks to serve per `getLogs` request. | `0` (no maximum) |
| `ws-cpu-refill-rate` | duration | The refill rate specifies the maximum amount of CPU time to allot a single connection per second. | `0` (no maximum) |
| `ws-cpu-max-stored` | duration | Specifies the maximum amount of CPU time that can be stored for a single WS connection. | `0` (no maximum) |
| `allow-unfinalized-queries` | bool | Allows queries for unfinalized (not yet accepted) blocks/transactions. | `false` |
| `accepted-cache-size` | integer | Specifies the depth to keep accepted headers and accepted logs in the cache. This is particularly useful to improve the performance of `eth_getLogs` for recent logs. | `32` |
| `http-body-limit` | integer | Maximum size in bytes for HTTP request bodies. | `0` (no maximum) |


## Database

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `state-scheme` | string | Can be one of `hash` or `firewood`. __WARNING__: `firewood` scheme is untested in production. | `hash` |
| `trie-clean-cache` | integer | Size of cache used for clean trie nodes (in MBs). Should be a multiple of `64`. | `512` |
| `trie-dirty-cache` | integer | Size of cache used for dirty trie nodes (in MBs). When the dirty nodes exceed this limit, they are written to disk. | `512` |
| `trie-dirty-commit-target` | integer | Memory limit to target in the dirty cache before performing a commit (in MBs). | `20` |
| `trie-prefetcher-parallelism` | integer | Max concurrent disk reads trie pre-fetcher should perform at once. | `16` |
| `snapshot-cache` | integer | Size of the snapshot disk layer clean cache (in MBs). Should be a multiple of `64`. | `256` |
| `acceptor-queue-limit` | integer | Specifies the maximum number of blocks to queue during block acceptance before blocking on Accept. | `64` |
| `commit-interval` | integer | Specifies the commit interval at which to persist the merkle trie to disk. | `4096` |
| `pruning-enabled` | bool | If `true`, database pruning of obsolete historical data will be enabled. This reduces the amount of data written to disk, but does not delete any state that is written to the disk previously. This flag should be set to `false` for nodes that need access to all data at historical roots. Pruning will be done only for new data. **Note**: If a node is ever run with `pruning-enabled` as `false` (archival mode), setting `pruning-enabled` to `true` will result in a warning and the node will shut down. This is to protect against unintentional misconfigurations of an archival node. To override this and switch to pruning mode, in addition to `pruning-enabled: true`, `allow-missing-tries` should be set to `true` as well. | `false` in v1.4.9, and `true` in subsequent versions |
| `populate-missing-tries` | _uint64_ | If non-nil, sets the starting point for repopulating missing tries to re-generate archival merkle forest. To restore an archival merkle forest that has been corrupted (missing trie nodes for a section of the blockchain), specify the starting point of the last block on disk, where the full trie was available at that block to re-process blocks from that height onwards and re-generate the archival merkle forest on startup. This flag should be used once to re-generate the archival merkle forest and should be removed from the config after completion. This flag will cause the node to delay starting up while it re-processes old blocks. | - |
| `populate-missing-tries-parallelism` | integer | Number of concurrent readers to use when re-populating missing tries on startup. | 1024 |
| `allow-missing-tries` | bool | If `true`, allows a node that was once configured as archival to switch to pruning mode. | `false` |
| `preimages-enabled` | bool | If `true`, enables preimages. | `false` |
| `prune-warp-db-enabled` | bool | If `true`, clears the warp database on startup. | `false` |
| `offline-pruning-enabled` | bool | If `true`, offline pruning will run on startup and block until it completes (approximately one hour on Mainnet). This will reduce the size of the database by deleting old trie nodes. **While performing offline pruning, your node will not be able to process blocks and will be considered offline.** While ongoing, the pruning process consumes a small amount of additional disk space (for deletion markers and the bloom filter). For more information see [here.](https://build.avax.network/docs/nodes/maintain/reduce-disk-usage#disk-space-considerations). Since offline pruning deletes old state data, this should not be run on nodes that need to support archival API requests. This is meant to be run manually, so after running with this flag once, it must be toggled back to false before running the node again. Therefore, you should run with this flag set to true and then set it to false on the subsequent run. | `false` |
| `offline-pruning-bloom-filter-size` | integer | This flag sets the size of the bloom filter to use in offline pruning (denominated in MB and defaulting to 512 MB). The bloom filter is kept in memory for efficient checks during pruning and is also written to disk to allow pruning to resume without re-generating the bloom filter. The active state is added to the bloom filter before iterating the DB to find trie nodes that can be safely deleted, any trie nodes not in the bloom filter are considered safe for deletion. The size of the bloom filter may impact its false positive rate, which can impact the results of offline pruning. This is an advanced parameter that has been tuned to 512 MB and should not be changed without thoughtful consideration. | 512 MB |
| `offline-pruning-data-directory` | string | This flag must be set when offline pruning is enabled and sets the directory that offline pruning will use to write its bloom filter to disk. This directory should not be changed in between runs until offline pruning has completed. | - |
| `transaction-history` | integer | Number of recent blocks for which to maintain transaction lookup indices in the database. If set to 0, transaction lookup indices will be maintained for all blocks. | `0` |
| `state-history` | integer | The maximum number of blocks from head whose state histories are reserved for pruning blockchains. | `32` |
| `historical-proof-query-window` | integer | When running in archive mode only, the number of blocks before the last accepted block to be accepted for proof state queries. | `43200` |
| `skip-tx-indexing` | bool | If set to `true`, the node will not index transactions. TxLookupLimit can be still used to control deleting old transaction indices. | `false` |
| `inspect-database` | bool | If set to `true`, inspects the database on startup. | `false` |

## Transaction Pool

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `local-txs-enabled` | bool | Enables local transaction handling (prioritizes transactions submitted through this node). | `false` |
| `allow-unprotected-txs` | bool | If `true`, the APIs will allow transactions that are not replay protected (EIP-155) to be issued through this node. | `false` |
| `allow-unprotected-tx-hashes` | \[\]TxHash | Specifies an array of transaction hashes that should be allowed to bypass replay protection. This flag is intended for node operators that want to explicitly allow specific transactions to be issued through their API. | an empty list |
| `price-options-slow-fee-percentage` | integer | Percentage to apply for slow fee estimation. | `95` |
| `price-options-fast-fee-percentage` | integer | Percentage to apply for fast fee estimation. | `105` |
| `price-options-max-tip` | integer | Maximum tip in wei for fee estimation. | `20000000000` (20 Gwei) |
| `push-gossip-percent-stake` | float | Percentage of the total stake to send transactions received over the RPC. | `0.9` |
| `push-gossip-num-validators` | integer | Number of validators to initially send transactions received over the RPC. | `100` |
| `push-gossip-num-peers` | integer | Number of peers to initially send transactions received over the RPC. | `0` |
| `push-regossip-num-validators` | integer | Number of validators to periodically send transactions received over the RPC. | `10` |
| `push-regossip-num-peers` | integer | Number of peers to periodically send transactions received over the RPC. | `0` |
| `push-gossip-frequency` | duration | Frequency in nanoseconds to send transactions received over the RPC to peers. | `100000000` (100 milliseconds) |
| `pull-gossip-frequency` | duration | Frequency in nanoseconds to request transactions from peers. | `1000000000` (1 second) |
| `regossip-frequency` | duration | Amount of time in seconds that should elapse before we attempt to re-gossip a transaction that was already gossiped once. | `30000000000` (30 seconds) |
| `tx-pool-price-limit` | integer | Minimum gas price in wei to enforce for acceptance into the pool. | `1` |
| `tx-pool-price-bump` | integer | Minimum price bump percentage to replace an already existing transaction (nonce). | `10%` |
| `tx-pool-account-slots` | integer | Number of executable transaction slots guaranteed per account. | `16` |
| `tx-pool-global-slots` | integer | Maximum number of executable transaction slots for all accounts. | `5120` |
| `tx-pool-account-queue` | integer | Maximum number of non-executable transaction slots permitted per account. | `64` |
| `tx-pool-global-queue` | integer | Maximum number of non-executable transaction slots for all accounts. | `1024` |
| `tx-pool-lifetime` | duration | Maximum duration in nanoseconds a non-executable transaction will be allowed in the poll | `600000000000` (10 minutes) |

## Snapshots

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `snapshot-wait` | bool | Waits for snapshot generation to complete before starting. | `false` |
| `snapshot-verification-enabled` | bool | Verifies the complete snapshot after it has been generated. | `false` |

## Logging

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `log-level` | string | Defines the log level for the chain. Must be one of `"trace"`, `"debug"`, `"info"`, `"warn"`, `"error"`, `"crit"`. | `"info"` |
| `log-json-format` | bool | If `true`, changes logs to JSON format. | `false` |

## Continuous Profiling

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `continuous-profiler-dir` | string | Enables the continuous profiler (captures a CPU/Memory/Lock profile at a specified interval). If a non-empty string is provided, it enables the continuous profiler and specifies the directory to place the profiles in. | `""` | 
| `continuous-profiler-frequency` | duration | Specifies the frequency to run the continuous profiler in nanoseconds. | `900000000000` (15 minutes) | 
| `continuous-profiler-max-files` | integer | Specifies the maximum number of profiles to keep before removing the oldest. |`5`| 

## Metrics

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `metrics-expensive-enabled` | bool | Enables expensive metrics. This includes Firewood metrics. | `true` |

## Keystore Settings

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `keystore-directory` | string | The directory that contains private keys. Can be given as a relative path. If empty, uses a temporary directory at `coreth-keystore`. | `""` |
| `keystore-external-signer` | string | Specifies an external URI for a clef-type signer.| `""` (not enabled) |
| `keystore-insecure-unlock-allowed` | bool | Allows users to unlock accounts in unsafe HTTP environment | `false` |

## VM Networking

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `max-outbound-active-requests` | integer | Specifies the maximum number of outbound VM2VM requests in flight at once. | `16` |

## State Sync

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `state-sync-enabled` | bool | Set to `true` to start the chain with state sync enabled. The peer will download chain state from peers up to a recent block near tip, then proceed with normal bootstrapping. Please note that if you need historical data, state sync isn't the right option. However, it is sufficient if you are just running a validator. | | perform state sync if starting a new node from scratch. However, if running with an existing database it will default to false and not perform state sync on subsequent runs. | 
| `state-sync-skip-resume` | bool | If set to `true`, the chain will not resume a previously started state sync operation that did not complete. Normally, the chain should be able to resume state syncing without any issue. | `false` | 
| `state-sync-min-blocks` | integer | Minimum number of blocks the chain should be ahead of the local node to prefer state syncing over bootstrapping. If the node's database is already close to the chain's tip, bootstrapping is more efficient. | `300000` | 
| `state-sync-ids` | string | Comma separated list of node IDs (prefixed with `NodeID-`) to fetch state sync data from. An example setting of this field would be `--state-sync-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"`. If not specified (or empty), peers are selected at random.| `""`| 
|  `state-sync-server-trie-cache` | integer | Size of trie cache used for providing state sync data to peers in MBs. Should be a multiple of `64`. | `64` | 
| `state-sync-commit-interval` | integer | Specifies the commit interval at which to persist EVM and atomic tries during state sync | `16384` | 
| `state-sync-request-size` | integer | The number of key/values to ask peers for per state sync request | `1024` | 

## Warp Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | _Array of Hex Strings_ | Encodes off-chain messages (unrelated to any on-chain event ie. block or AddressedCall) that the node should be willing to sign. Note: only supports AddressedCall payloads. | empty array |

## Miscellaneous

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `skip-upgrade-check` | bool | If set to `true`, the chain will skip verifying that all expected network upgrades have taken place before the last accepted block on startup. This allows node operators to recover if their node has accepted blocks after a network upgrade with a version of the code prior to the upgrade. | `false` |

## Gas Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `gas-target` | integer | The target gas per second that this node will attempt to use when creating blocks | Parent block's target

## Enabling Avalanche Specific APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `admin-api-enabled` | bool | Enables the Admin API |  `false` | 
| `admin-api-dir` | string | Specifies the directory for the Admin API to use to store CPU/Mem/Lock Profiles | `""` | 
| `warp-api-enabled`  | bool | Enables the Warp API | `false`

### Enabling EVM APIs

> **Note**: The names used in this configuration flag have been updated in Coreth `v0.8.14`. The previous names containing `public-` and `private-` are deprecated. While the current version continues to accept deprecated values, they may not be supported in future updates and updating to the new values is recommended.
>
> The mapping of deprecated values and their updated equivalent follows:
>
> |Deprecated                      |Use instead         |
> |--------------------------------|--------------------|
> |public-eth                      |eth                 |
> |public-eth-filter               |eth-filter          |
> |private-admin                   |admin               |
> |private-debug                   |debug               |
> |public-debug                    |debug               |
> |internal-public-eth             |internal-eth        |
> |internal-public-blockchain      |internal-blockchain |
> |internal-public-transaction-pool|internal-transaction|
> |internal-public-tx-pool         |internal-tx-pool    |
> |internal-public-debug           |internal-debug      |
> |internal-private-debug          |internal-debug      |
> |internal-public-account         |internal-account    |
> |internal-private-personal       |internal-personal   |

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `eth-apis` | \[\]string | Specify the exact set of below services to enable on your node. If you populate this field, it will override the defaults so you must include every service you wish to enable. | `["eth","eth-filter","net","web3","internal-eth","internal-blockchain","internal-transaction"]` | 
| `eth` | bool | Adds the `eth_coinbase` and `eth_etherbase` RPC calls to the `eth_*` namespace. | `true` | 
| `eth-filter` | bool |  Enables the public filter API for the `eth_*` namespace and adds the following RPC calls (see [here](https://eth.wiki/json-rpc/API) for complete documentation): <br/> - `eth_newPendingTransactionFilter` <br/> - `eth_newPendingTransactions` <br/> - `eth_newAcceptedTransactions` <br/> - `eth_newBlockFilter` <br/> - `eth_newHeads` <br/> - `eth_logs` <br/> - `eth_newFilter` <br/> - `eth_getLogs` <br/> - `eth_uninstallFilter` <br/> - `eth_getFilterLogs` <br/> - `eth_getFilterChanges` <br/> |   `true` | 
| `admin` | bool | Adds the `admin_importChain` and `admin_exportChain` RPC calls to the `admin_*` namespace | `false` | 
| `debug` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_dumpBlock` <br/> - `debug_accountRange` <br/> - `debug_preimage` <br/> - `debug_getBadBlocks` <br/> - `debug_storageRangeAt` <br/> - `debug_getModifiedAccountsByNumber` <br/> - `debug_getModifiedAccountsByHash` <br/> - `debug_getAccessibleState` <br/> The following RPC calls are disabled for any nodes with `state-scheme = firewood`: <br/> - `debug_storageRangeAt` <br/> - `debug_getModifiedAccountsByNumber` <br/> - `debug_getModifiedAccountsByHash` <br/> | `false`.
| `net` | bool | Adds the following RPC calls to the `net_*` namespace. <br/> - `net_listening` <br/> - `net_peerCount` <br/> - `net_version` <br/> Note: Coreth is a virtual machine and does not have direct access to the networking layer, so `net_listening` always returns true and `net_peerCount` always returns 0. For accurate metrics on the network layer, users should use the AvalancheGo APIs. | `true` |
| `debug-tracer` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_traceChain` <br/> - `debug_traceBlockByNumber` <br/> - `debug_traceBlockByHash` <br/> - `debug_traceBlock` <br/> - `debug_traceBadBlock` <br/> - `debug_intermediateRoots` <br/> - `debug_traceTransaction` <br/> - `debug_traceCall` | `false` |
| `web3` | bool | Adds the `web3_clientVersion` and `web3_sha3` RPC calls to the `web3_*` namespace | `true` |
| `internal-eth` | bool | Adds the following RPC calls to the `eth_*` namespace. <br/> - `eth_gasPrice` <br/> - `eth_baseFee` <br/> - `eth_maxPriorityFeePerGas` <br/> - `eth_feeHistory` | `true` |
| `internal-blockchain` | bool | Adds the following RPC calls to the `eth_*` namespace. <br/> - `eth_chainId` <br/> - `eth_blockNumber` <br/> - `eth_getBalance` <br/> - `eth_getProof` <br/> - `eth_getHeaderByNumber` <br/> - `eth_getHeaderByHash` <br/> - `eth_getBlockByNumber` <br/> - `eth_getBlockByHash` <br/> - `eth_getUncleBlockByNumberAndIndex` <br/> - `eth_getUncleBlockByBlockHashAndIndex` <br/> - `eth_getUncleCountByBlockNumber` <br/> - `eth_getUncleCountByBlockHash` <br/> - `eth_getCode` <br/> - `eth_getStorageAt` <br/> - `eth_call` <br/> - `eth_estimateGas` <br/> - `eth_createAccessList` <br/> `eth_getProof` is disabled for any node with `state-scheme = firewood` | `true` |
| `internal-transaction` | bool | Adds the following RPC calls to the `eth_*` namespace. <br/> - `eth_getBlockTransactionCountByNumber` <br/> - `eth_getBlockTransactionCountByHash` <br/> - `eth_getTransactionByBlockNumberAndIndex` <br/> - `eth_getTransactionByBlockHashAndIndex` <br/> - `eth_getRawTransactionByBlockNumberAndIndex` <br/> - `eth_getRawTransactionByBlockHashAndIndex` <br/> - `eth_getTransactionCount` <br/> - `eth_getTransactionByHash` <br/> - `eth_getRawTransactionByHash` <br/> - `eth_getTransactionReceipt` <br/> - `eth_sendTransaction` <br/> - `eth_fillTransaction` <br/> - `eth_sendRawTransaction` <br/> - `eth_sign` <br/> - `eth_signTransaction` <br/> - `eth_pendingTransactions` <br/> - `eth_resend` | `true` |
| `internal-tx-pool` | bool | Adds the following RPC calls to the `txpool_*` namespace. <br/> - `txpool_content` <br/> - `txpool_contentFrom` <br/> - `txpool_status` <br/> - `txpool_inspect` | `false` |
| `internal-debug` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_getHeaderRlp` <br/> - `debug_getBlockRlp` <br/> - `debug_printBlock` <br/> - `debug_chaindbProperty` <br/> - `debug_chaindbCompact` | `false` |
| `debug-handler` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_verbosity` <br/> - `debug_vmodule` <br/> - `debug_backtraceAt` <br/> - `debug_memStats` <br/> - `debug_gcStats` <br/> - `debug_blockProfile` <br/> - `debug_setBlockProfileRate` <br/> - `debug_writeBlockProfile` <br/> - `debug_mutexProfile` <br/> - `debug_setMutexProfileFraction` <br/> - `debug_writeMutexProfile` <br/> - `debug_writeMemProfile` <br/> - `debug_stacks` <br/> - `debug_freeOSMemory` <br/> - `debug_setGCPercent` | `false` |
| `internal-account` | bool | Adds the `eth_accounts` RPC call to the `eth_*` namespace  | `true` |
| `internal-personal` | bool | Adds the following RPC calls to the `personal_*` namespace. <br/> - `personal_listAccounts` <br/> - `personal_listWallets` <br/> - `personal_openWallet` <br/> - `personal_deriveAccount` <br/> - `personal_newAccount` <br/> - `personal_importRawKey` <br/> - `personal_unlockAccount` <br/> - `personal_lockAccount` <br/> - `personal_sendTransaction` <br/> - `personal_signTransaction` <br/> - `personal_sign` <br/> - `personal_ecRecover` <br/> - `personal_signAndSendTransaction` <br/> - `personal_initializeWallet` <br/> - `personal_unpair` | `false` |

