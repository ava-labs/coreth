// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"github.com/ava-labs/coreth/params"
)

// ParsePredicates returns the predicates from a header's Extra field.
func ParsePredicates(rules params.AvalancheRules, extra []byte) ([]byte, error) {
	if err := VerifyExtra(rules, extra); err != nil {
		return nil, err
	}

	var offset int
	if rules.IsApricotPhase3 && !rules.IsFUpgrade {
		offset = FeeWindowSize
	}
	if rules.IsFUpgrade {
		offset = DynamicFeeAccumulatorSize
	}
	if rules.IsDurango {
		return extra[offset:], nil
	}
	return nil, nil
}
