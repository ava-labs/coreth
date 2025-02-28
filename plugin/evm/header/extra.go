// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params/extras"
)

const (
	// Note: MaximumExtraDataSize has been reduced to 32 in Geth, but is kept the same in Coreth for
	// backwards compatibility.
	MaximumExtraDataSize uint64 = 64 // Maximum size extra data may be after Genesis.
)

var errInvalidExtraLength = errors.New("invalid header.Extra length")

// ExtraPrefix takes the previous header and the timestamp of its child block
// and calculates the expected extra prefix for the child block.
func ExtraPrefix(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) ([]byte, error) {
	switch {
	case config.IsApricotPhase3(timestamp):
		window, err := feeWindow(config, parent, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return feeWindowBytes(window), nil
	default:
		// Prior to AP3 there was no expected extra prefix.
		return nil, nil
	}
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// [rules].
func VerifyExtra(rules extras.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsDurango:
		if extraLen < FeeWindowSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				FeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase3:
		if extraLen != FeeWindowSize {
			return fmt.Errorf(
				"%w: expected %d but got %d",
				errInvalidExtraLength,
				FeeWindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase1:
		if extraLen != 0 {
			return fmt.Errorf(
				"%w: expected 0 but got %d",
				errInvalidExtraLength,
				extraLen,
			)
		}
	default:
		if uint64(extraLen) > MaximumExtraDataSize {
			return fmt.Errorf(
				"%w: expected <= %d but got %d",
				errInvalidExtraLength,
				MaximumExtraDataSize,
				extraLen,
			)
		}
	}
	return nil
}

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(_ extras.AvalancheRules, extra []byte) []byte {
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to this size.
	// After Durango, the VM pre-verifies the extra data past the dynamic fee
	// rollup window is valid.
	if len(extra) <= FeeWindowSize {
		return nil
	}
	return extra[FeeWindowSize:]
}

// SetPredicateBytesInExtra sets the predicate result bytes in the header's extra
// data. If the extra data is not long enough (i.e., an incomplete header.Extra
// as built in the miner), it is padded with zeros.
func SetPredicateBytesInExtra(extra []byte, predicateBytes []byte) []byte {
	if len(extra) < FeeWindowSize {
		// pad extra with zeros
		extra = append(extra, make([]byte, FeeWindowSize-len(extra))...)
	}
	extra = extra[:FeeWindowSize] // truncate extra to FeeWindowSize
	extra = append(extra, predicateBytes...)
	return extra
}
