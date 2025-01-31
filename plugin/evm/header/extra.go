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

	ErrUnstructuredDataNotSupported = errors.New("unstructured data is not supported")
	ErrDynamicFeeWindowNotSupported = errors.New("dynamic fee window is not supported")
	ErrPredicatesNotSupported       = errors.New("predicates are not supported")
)

type Extra struct {
	// Unstructured data was introduced at genesis and removed in ApricotPhase1.
	Unstructured []byte
	// DynamicFeeWindow was introduced in ApricotPhase3.
	DynamicFeeWindow DynamicFeeWindow
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
	if rules.IsApricotPhase3 {
		var err error
		e.DynamicFeeWindow, err = ParseDynamicFeeWindow(extra)
		if err != nil {
			return Extra{}, err
		}
	}
	if rules.IsDurango {
		e.Predicates = extra[DynamicFeeWindowSize:]
	}
	return e, nil
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// [rules].
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
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
	if !rules.IsApricotPhase3 && e.DynamicFeeWindow != (DynamicFeeWindow{}) {
		return ErrDynamicFeeWindowNotSupported
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
	if rules.IsApricotPhase3 {
		result = append(result, e.DynamicFeeWindow.Bytes()...)
	}
	if rules.IsDurango {
		result = append(result, e.Predicates...)
	}
	return result
}
