// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

var (
	ErrTimestampMillisecondsRequired      = errors.New("TimestampMilliseconds is required after Granite activation")
	ErrTimestampMillisecondsBeforeGranite = errors.New("TimestampMilliseconds should be nil before Granite activation")
	ErrTimestampMillisecondsMismatch      = errors.New("timestamp milliseconds mismatch")
)

func TimestampMilliseconds(rulesExtra extras.AvalancheRules, tstart time.Time) *uint64 {
	if !rulesExtra.IsGranite {
		return nil
	}
	// Extract milliseconds portion from tstart
	timestampMilliseconds := uint64(tstart.UnixMilli())
	return &timestampMilliseconds
}

func VerifyTimestampMilliseconds(rulesExtra extras.AvalancheRules, header *types.Header) error {
	headerExtra := customtypes.GetHeaderExtra(header)
	switch {
	case rulesExtra.IsGranite:
		if headerExtra.TimestampMilliseconds == nil {
			return ErrTimestampMillisecondsRequired
		}
		// Convert timestamp from milliseconds to seconds
		// and validate it matches the header time in seconds
		timestampMillisecondsToSeconds := *headerExtra.TimestampMilliseconds / 1000
		if header.Time != timestampMillisecondsToSeconds {
			return fmt.Errorf("%w: have %d, want %d", ErrTimestampMillisecondsMismatch, header.Time, timestampMillisecondsToSeconds)
		}
	default:
		// Before Granite, TimestampMilliseconds should be nil
		if headerExtra.TimestampMilliseconds != nil {
			return ErrTimestampMillisecondsBeforeGranite
		}
	}
	return nil
}
