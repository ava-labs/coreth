// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"github.com/ava-labs/libevm/common"
)

func (r *RulesExtra) PredicatersExist() bool {
	return len(r.Predicaters) > 0
}

func (r *RulesExtra) PredicaterExists(addr common.Address) bool {
	_, ok := r.Predicaters[addr]
	return ok
}

// IsPrecompileEnabled returns true if the precompile at [addr] is enabled for this rule set.
func (r *RulesExtra) IsPrecompileEnabled(addr common.Address) bool {
	_, ok := r.Precompiles[addr]
	return ok
}
