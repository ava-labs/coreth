package clienttypes

type NetworkUpgrades struct {
	ApricotPhase1BlockTimestamp *uint64 `json:"apricotPhase1BlockTimestamp,omitempty"` // Apricot Phase 1 Block Timestamp (nil = no fork, 0 = already activated)
	// Apricot Phase 2 Block Timestamp (nil = no fork, 0 = already activated)
	// Apricot Phase 2 includes a modified version of the Berlin Hard Fork from Ethereum
	ApricotPhase2BlockTimestamp *uint64 `json:"apricotPhase2BlockTimestamp,omitempty"`
	// Apricot Phase 3 introduces dynamic fees and a modified version of the London Hard Fork from Ethereum (nil = no fork, 0 = already activated)
	ApricotPhase3BlockTimestamp *uint64 `json:"apricotPhase3BlockTimestamp,omitempty"`
	// Apricot Phase 4 introduces the notion of a block fee to the dynamic fee algorithm (nil = no fork, 0 = already activated)
	ApricotPhase4BlockTimestamp *uint64 `json:"apricotPhase4BlockTimestamp,omitempty"`
	// Apricot Phase 5 introduces a batch of atomic transactions with a maximum atomic gas limit per block. (nil = no fork, 0 = already activated)
	ApricotPhase5BlockTimestamp *uint64 `json:"apricotPhase5BlockTimestamp,omitempty"`
	// Apricot Phase Pre-6 deprecates the NativeAssetCall precompile (soft). (nil = no fork, 0 = already activated)
	ApricotPhasePre6BlockTimestamp *uint64 `json:"apricotPhasePre6BlockTimestamp,omitempty"`
	// Apricot Phase 6 deprecates the NativeAssetBalance and NativeAssetCall precompiles. (nil = no fork, 0 = already activated)
	ApricotPhase6BlockTimestamp *uint64 `json:"apricotPhase6BlockTimestamp,omitempty"`
	// Apricot Phase Post-6 deprecates the NativeAssetCall precompile (soft). (nil = no fork, 0 = already activated)
	ApricotPhasePost6BlockTimestamp *uint64 `json:"apricotPhasePost6BlockTimestamp,omitempty"`
	// Banff restricts import/export transactions to AVAX. (nil = no fork, 0 = already activated)
	BanffBlockTimestamp *uint64 `json:"banffBlockTimestamp,omitempty"`
	// Cortina increases the block gas limit to 15M. (nil = no fork, 0 = already activated)
	CortinaBlockTimestamp *uint64 `json:"cortinaBlockTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging. (nil = no fork, 0 = already activated)
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoBlockTimestamp *uint64 `json:"durangoBlockTimestamp,omitempty"`
	// Etna activates Cancun from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/cancun.md#included-eips)
	// and reduces the min base fee. (nil = no fork, 0 = already activated)
	// Note: EIP-4844 BlobTxs are not enabled in the mempool and blocks are not
	// allowed to contain them. For details see https://github.com/avalanche-foundation/ACPs/pull/131
	EtnaTimestamp *uint64 `json:"etnaTimestamp,omitempty"`
}
