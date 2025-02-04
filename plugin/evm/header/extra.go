// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/params"
)

var (
	ErrInvalidExtraLength = errors.New("invalid header.Extra length")

	ErrUnstructuredDataNotSupported      = errors.New("unstructured data is not supported")
	ErrDynamicFeeWindowNotSupported      = errors.New("dynamic fee window is not supported")
	ErrDynamicFeeAccumulatorNotSupported = errors.New("dynamic fee accumulator is not supported")
	ErrPredicatesNotSupported            = errors.New("predicates are not supported")
)

type Extra struct {
	// Unstructured data was introduced at genesis and removed in ApricotPhase1.
	Unstructured []byte
	// DynamicFeeWindow was introduced in ApricotPhase3 and removed in FUpgrade.
	DynamicFeeWindow DynamicFeeWindow
	// DynamicFeeWindow was introduced in FUpgrade.
	DynamicFeeAccumulator DynamicFeeAccumulator
	// Predicates was introduced in Durango.
	Predicates []byte
}

func ParseExtra(rules params.AvalancheRules, extra []byte) (Extra, error) {
	if err := VerifyExtra(rules, extra); err != nil {
		return Extra{}, err
	}

	var e Extra
	if !rules.IsApricotPhase1 {
		e.Unstructured = extra
	}
	var offset int
	if rules.IsApricotPhase3 && !rules.IsFUpgrade {
		var err error
		e.DynamicFeeWindow, err = ParseDynamicFeeWindow(extra)
		if err != nil {
			return Extra{}, err
		}
		offset = DynamicFeeWindowSize
	}
	if rules.IsFUpgrade {
		var err error
		e.DynamicFeeAccumulator, err = ParseDynamicFeeAccumulator(extra)
		if err != nil {
			return Extra{}, err
		}
		offset = ErrDynamicFeeAccumulatorSize
	}
	if rules.IsDurango {
		e.Predicates = extra[offset:]
	}
	return e, nil
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// [rules].
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsFUpgrade:
		if extraLen < ErrDynamicFeeAccumulatorSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				ErrInvalidExtraLength,
				ErrDynamicFeeAccumulatorSize,
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

func (e *Extra) Verify(rules params.AvalancheRules) error {
	if rules.IsApricotPhase1 && len(e.Unstructured) != 0 {
		return ErrUnstructuredDataNotSupported
	}
	if (!rules.IsApricotPhase3 || rules.IsFUpgrade) && e.DynamicFeeWindow != (DynamicFeeWindow{}) {
		return ErrDynamicFeeWindowNotSupported
	}
	if !rules.IsFUpgrade && e.DynamicFeeAccumulator != (DynamicFeeAccumulator{}) {
		return ErrDynamicFeeAccumulatorNotSupported
	}
	if !rules.IsDurango && len(e.Predicates) != 0 {
		return ErrPredicatesNotSupported
	}
	return nil
}

func (e *Extra) Bytes(rules params.AvalancheRules) []byte {
	var result []byte
	if !rules.IsApricotPhase1 {
		result = append(result, e.Unstructured...)
	}
	if rules.IsApricotPhase3 && !rules.IsFUpgrade {
		result = append(result, e.DynamicFeeWindow.Bytes()...)
	}
	if rules.IsFUpgrade {
		result = append(result, e.DynamicFeeAccumulator.Bytes()...)
	}
	if rules.IsDurango {
		result = append(result, e.Predicates...)
	}
	return result
}
