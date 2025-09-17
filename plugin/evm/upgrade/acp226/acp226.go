// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP-226 implements the dynamic minimum block delay mechanism specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package acp226

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/coreth/plugin/evm/upgrade/common"
)

const (
	// MinTargetDelayMilliseconds (M) is the minimum target block delay in milliseconds
	MinTargetDelayMilliseconds = 1 // ms
	// TargetConversion (D) is the conversion factor for exponential calculations
	TargetConversion = 1 << 20
	// MaxTargetDelayExcessDiff (Q) is the maximum change in target excess per update
	MaxTargetDelayExcessDiff = 200

	StateSize = wrappers.LongLen

	maxTargetDelayExcess = 46_516_320 // TargetConversion * ln(MaxUint64 / MinTargetDelayMilliseconds) + 1
)

var (
	params = common.TargetExcessParams{
		MinTarget:        MinTargetDelayMilliseconds,
		TargetConversion: TargetConversion,
		MaxExcessDiff:    MaxTargetDelayExcessDiff,
		MaxExcess:        maxTargetDelayExcess,
	}

	ErrStateInsufficientLength = errors.New("insufficient length for block delay state")
)

// TargetDelayExcess represents the target excess for delay calculation in the dynamic minimum block delay mechanism.
type TargetDelayExcess uint64

// ParseTargetDelayExcess returns the target delay excess from the provided bytes. It is the inverse of
// [TargetDelayExcess.Bytes]. This function allows for additional bytes to be padded at the
// end of the provided bytes.
func ParseTargetDelayExcess(bytes []byte) (TargetDelayExcess, error) {
	if len(bytes) < StateSize {
		return 0, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrStateInsufficientLength,
			StateSize,
			len(bytes),
		)
	}

	return TargetDelayExcess(binary.BigEndian.Uint64(bytes)), nil
}

// Bytes returns the binary representation of the target delay excess.
func (t TargetDelayExcess) Bytes() []byte {
	bytes := make([]byte, StateSize)
	binary.BigEndian.PutUint64(bytes, uint64(t))
	return bytes
}

// TargetDelay returns the target minimum block delay in milliseconds, `T`.
//
// TargetDelay = MinTargetDelayMilliseconds * e^(TargetDelayExcess / TargetConversion)
func (t TargetDelayExcess) TargetDelay() uint64 {
	return params.CalculateTarget(uint64(t))
}

// UpdateTargetDelayExcess updates the targetDelayExcess to be as close as possible to the
// desiredTargetDelayExcess without exceeding the maximum targetDelayExcess change.
func (t *TargetDelayExcess) UpdateTargetDelayExcess(desiredTargetDelayExcess uint64) {
	*t = TargetDelayExcess(params.TargetExcess(uint64(*t), desiredTargetDelayExcess))
}

// DesiredTargetDelayExcess calculates the optimal desiredTargetDelayExcess given the
// desired target delay.
func DesiredTargetDelayExcess(desiredTargetDelayExcess uint64) uint64 {
	return params.DesiredTargetExcess(desiredTargetDelayExcess)
}
