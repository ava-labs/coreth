package params

import (
	"math/big"

	ethparams "github.com/ava-labs/libevm/params"
)

// Avalanche ChainIDs
var (
	// AvalancheMainnetChainID ...
	AvalancheMainnetChainID = big.NewInt(43114)
	// AvalancheFujiChainID ...
	AvalancheFujiChainID = big.NewInt(43113)
	// AvalancheLocalChainID ...
	AvalancheLocalChainID = big.NewInt(43112)
)

type Rules = ethparams.Rules
