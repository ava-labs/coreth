// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/libevm/common"
)

type RulesExtra struct {
	// Rules for Avalanche releases
	AvalancheRules

	// Precompiles maps addresses to stateful precompiled contracts that are enabled
	// for this rule set.
	// Note: none of these addresses should conflict with the address space used by
	// any existing precompiles.
	Precompiles map[common.Address]precompileconfig.Config
	// Predicaters maps addresses to stateful precompile Predicaters
	// that are enabled for this rule set.
	Predicaters map[common.Address]precompileconfig.Predicater
	// AccepterPrecompiles map addresses to stateful precompile accepter functions
	// that are enabled for this rule set.
	AccepterPrecompiles map[common.Address]precompileconfig.Accepter
}
