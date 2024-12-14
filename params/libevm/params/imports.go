package params

import "github.com/ava-labs/coreth/params"

// These are imported from params so this package can be used as a drop-in replacement for the params package.
const (
	MaxGasLimit           = params.MaxGasLimit
	MinGasLimit           = params.MinGasLimit
	ApricotPhase1GasLimit = params.ApricotPhase1GasLimit
	CortinaGasLimit       = params.CortinaGasLimit
	MaxCodeSize           = params.MaxCodeSize
	RollupWindow          = params.RollupWindow

	GasLimitBoundDivisor    = params.GasLimitBoundDivisor
	MaximumExtraDataSize    = params.MaximumExtraDataSize
	DynamicFeeExtraDataSize = params.DynamicFeeExtraDataSize

	ApricotPhase3MinBaseFee     = params.ApricotPhase3MinBaseFee
	ApricotPhase3MaxBaseFee     = params.ApricotPhase3MaxBaseFee
	ApricotPhase3InitialBaseFee = params.ApricotPhase3InitialBaseFee
	ApricotPhase3TargetGas      = params.ApricotPhase3TargetGas
	ApricotPhase5TargetGas      = params.ApricotPhase5TargetGas

	LaunchMinGasPrice                     = params.LaunchMinGasPrice
	AvalancheAtomicTxFee                  = params.AvalancheAtomicTxFee
	ApricotPhase4MinBaseFee               = params.ApricotPhase4MinBaseFee
	ApricotPhase4MaxBaseFee               = params.ApricotPhase4MaxBaseFee
	ApricotPhase5BaseFeeChangeDenominator = params.ApricotPhase5BaseFeeChangeDenominator
	EtnaMinBaseFee                        = params.EtnaMinBaseFee

	GenesisGasLimit      = params.GenesisGasLimit
	ColdSloadCostEIP2929 = params.ColdSloadCostEIP2929

	GWei  = params.GWei
	Ether = params.Ether
	TxGas = params.TxGas

	IsMergeTODO = params.IsMergeTODO
)

var (
	AtomicGasLimit = params.AtomicGasLimit
)

type (
	PrecompileUpgrade = params.PrecompileUpgrade
)
