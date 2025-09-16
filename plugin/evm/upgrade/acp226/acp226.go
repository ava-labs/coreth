// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP-226 implements the dynamic minimum block delay mechanism specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package acp226

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MinTargetDelayMilliseconds (M) is the minimum target block delay in milliseconds
	MinTargetDelayMilliseconds = 1 // ms
	// TargetConversion (D) is the conversion factor for exponential calculations
	TargetConversion = 1 << 20
	// MaxTargetExcessDiff (Q) is the maximum change in target excess per update
	MaxTargetExcessDiff = 200

	StateSize = wrappers.LongLen

	maxTargetExcess = 46_516_320 // TargetConversion * ln(MaxUint64 / MinTargetDelayMilliseconds) + 1
)

var ErrStateInsufficientLength = errors.New("insufficient length for block delay state")

// State represents the current state of the dynamic minimum block delay mechanism.
type State struct {
	TargetExcess uint64 // q - target excess for delay calculation
}

// ParseState returns the state from the provided bytes. It is the inverse of
// [State.Bytes]. This function allows for additional bytes to be padded at the
// end of the provided bytes.
func ParseMinimumBlockDelayExcess(bytes []byte) (State, error) {
	if len(bytes) < StateSize {
		return State{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrStateInsufficientLength,
			StateSize,
			len(bytes),
		)
	}

	return State{
		TargetExcess: binary.BigEndian.Uint64(bytes),
	}, nil
}

// Bytes returns the binary representation of the state.
func (s *State) Bytes() []byte {
	bytes := make([]byte, StateSize)
	binary.BigEndian.PutUint64(bytes, s.TargetExcess)
	return bytes
}

// TargetDelay returns the target minimum block delay in milliseconds, `T`.
//
// TargetDelay = MinTargetDelayMilliseconds * e^(TargetExcess / TargetConversion)
func (s *State) TargetDelay() uint64 {
	return uint64(gas.CalculatePrice(
		MinTargetDelayMilliseconds,
		gas.Gas(s.TargetExcess),
		TargetConversion,
	))
}

// UpdateTargetExcess updates the targetExcess to be as close as possible to the
// desiredTargetExcess without exceeding the maximum targetExcess change.
func (s *State) UpdateTargetExcess(desiredTargetExcess uint64) {
	s.TargetExcess = targetExcess(s.TargetExcess, desiredTargetExcess)
}

// DesiredTargetExcess calculates the optimal desiredTargetExcess given the
// desired target delay.
func DesiredTargetExcess(desiredTarget uint64) uint64 {
	// This could be solved directly by calculating D * ln(desiredTarget / M)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return uint64(sort.Search(maxTargetExcess, func(targetExcessGuess int) bool {
		state := State{
			TargetExcess: uint64(targetExcessGuess),
		}
		return state.TargetDelay() >= desiredTarget
	}))
}

// targetExcess calculates the optimal new targetExcess for a block proposer to
// include given the current and desired excess values.
func targetExcess(excess, desired uint64) uint64 {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxTargetExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}
