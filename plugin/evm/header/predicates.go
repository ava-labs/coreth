// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/params"
)

var ErrInvalidExtraLength = errors.New("invalid header.Extra length")

func ParsePredicates(rules params.AvalancheRules, extra []byte) ([]byte, error) {
	if err := VerifyExtra(rules, extra); err != nil {
		return nil, err
	}

	var offset int
	if rules.IsApricotPhase3 && !rules.IsFUpgrade {
		offset = DynamicFeeWindowSize
	}
	if rules.IsFUpgrade {
		offset = DynamicFeeAccumulatorSize
	}
	if rules.IsDurango {
		return extra[offset:], nil
	}
	return nil, nil
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// [rules].
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsFUpgrade:
		if extraLen < DynamicFeeAccumulatorSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				ErrInvalidExtraLength,
				DynamicFeeAccumulatorSize,
				extraLen,
			)
		}
	case rules.IsDurango:
		if extraLen < DynamicFeeWindowSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				ErrInvalidExtraLength,
				DynamicFeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase3:
		if extraLen != DynamicFeeWindowSize {
			return fmt.Errorf(
				"%w: expected %d but got %d",
				ErrInvalidExtraLength,
				DynamicFeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase1:
		if extraLen != 0 {
			return fmt.Errorf(
				"%w: expected 0 but got %d",
				ErrInvalidExtraLength,
				extraLen,
			)
		}
	default:
		if uint64(extraLen) > params.MaximumExtraDataSize {
			return fmt.Errorf(
				"%w: expected <= %d but got %d",
				ErrInvalidExtraLength,
				params.MaximumExtraDataSize,
				extraLen,
			)
		}
	}
	return nil
}
