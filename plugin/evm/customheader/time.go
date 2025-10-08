// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

var (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	MaxFutureBlockTime = 10 * time.Second

	errBlockTooOld                   = errors.New("block timestamp is too old")
	ErrBlockTooFarInFuture           = errors.New("block timestamp is too far in the future")
	ErrTimeMillisecondsRequired      = errors.New("TimeMilliseconds is required after Granite activation")
	ErrTimeMillisecondsMismatched    = errors.New("TimeMilliseconds does not match header.Time")
	ErrTimeMillisecondsBeforeGranite = errors.New("TimeMilliseconds should be nil before Granite activation")
	ErrMinDelayNotMet                = errors.New("minimum block delay not met")
	ErrGraniteClockBehindParent      = errors.New("current timestamp is not allowed to be behind than parent timestamp in Granite")
)

// GetNextTimestamp calculates the timestamp (in seconds and milliseconds) for the header based on the parent's timestamp and the current time.
// It returns an error if the timestamp is behind the parent's timestamp in Granite.
// First return value is the timestamp in seconds, second return value is the timestamp in milliseconds.
func GetNextTimestamp(parent *types.Header, config *extras.ChainConfig, now time.Time) (uint64, uint64, error) {
	var (
		timestamp   = uint64(now.Unix())
		timestampMS = uint64(now.UnixMilli())
	)
	// This should be the expected/normal case.
	if parent.Time < timestamp {
		return timestamp, timestampMS, nil
	}
	// In Granite, there is a minimum delay enforced, and if the timestamp is the same as the parent,
	// the block will be rejected. This is to early-exit from the block building.
	// The block builder should have already waited enough time to meet the minimum delay.
	if config.IsGranite(timestamp) {
		// Timestamp milliseconds might be still later than parent's timestamp milliseconds
		// even though the second timestamp is equal to the parent's timestamp. (from the case parent.Time < timestamp)
		parentTimeMS := customtypes.HeaderTimeMilliseconds(parent)
		if timestampMS <= parentTimeMS {
			return 0, 0, fmt.Errorf("%w: current %d <= parent %d", ErrGraniteClockBehindParent, timestampMS, parentTimeMS)
		}
		return timestamp, timestampMS, nil
	}
	// In pre-Granite, blocks are allowed to have the same timestamp as their parent
	// Actually we don't need to return modified timestampMS, because it will be not be set if this is not
	// Granite, but setting here for consistency.
	return parent.Time, parent.Time * 1000, nil
}

// VerifyTime verifies that the header's Time and TimeMilliseconds fields are
// consistent with the given rules and the current time.
// This includes:
// - TimeMilliseconds is nil before Granite activation
// - TimeMilliseconds is non-nil after Granite activation
// - Time matches TimeMilliseconds/1000 after Granite activation
// - Time/TimeMilliseconds is not too far in the future
// - Time/TimeMilliseconds is non-decreasing
// - Minimum block delay is enforced
func VerifyTime(extraConfig *extras.ChainConfig, parent *types.Header, header *types.Header, now time.Time) error {
	var (
		headerExtra = customtypes.GetHeaderExtra(header)
		parentExtra = customtypes.GetHeaderExtra(parent)
	)

	// Verify the header's timestamp is not earlier than parent's
	// it does include equality(==), so multiple blocks per second is ok
	if header.Time < parent.Time {
		return fmt.Errorf("%w: %d < parent %d", errBlockTooOld, header.Time, parent.Time)
	}

	// Do all checks that apply only before Granite
	if !extraConfig.IsGranite(header.Time) {
		// Make sure the block isn't too far in the future
		if maxBlockTime := uint64(now.Add(MaxFutureBlockTime).Unix()); header.Time > maxBlockTime {
			return fmt.Errorf("%w: %d > allowed %d", ErrBlockTooFarInFuture, header.Time, maxBlockTime)
		}

		// This field should not be set yet.
		if headerExtra.TimeMilliseconds != nil {
			return ErrTimeMillisecondsBeforeGranite
		}
		return nil
	}

	// After Granite, TimeMilliseconds is required
	if headerExtra.TimeMilliseconds == nil {
		return ErrTimeMillisecondsRequired
	}

	// Validate TimeMilliseconds is consistent with header.Time
	if expectedTime := *headerExtra.TimeMilliseconds / 1000; header.Time != expectedTime {
		return fmt.Errorf("%w: header.Time (%d) != TimeMilliseconds/1000 = (%d)",
			ErrTimeMillisecondsMismatched,
			header.Time,
			expectedTime,
		)
	}

	// Verify TimeMilliseconds is not earlier than parent's TimeMilliseconds
	if parentExtra.TimeMilliseconds != nil && *headerExtra.TimeMilliseconds < *parentExtra.TimeMilliseconds {
		return fmt.Errorf("%w: %d < parent %d",
			errBlockTooOld,
			*headerExtra.TimeMilliseconds,
			*parentExtra.TimeMilliseconds,
		)
	}

	// Verify TimeMilliseconds is not too far in the future
	// Question (ceyonur): This still is a problem especially if someone issues a block in the future
	// then the next builder will wait until potentially MaxFutureBlockTime + delay
	if maxBlockTimeMillis := uint64(now.Add(MaxFutureBlockTime).UnixMilli()); *headerExtra.TimeMilliseconds > maxBlockTimeMillis {
		return fmt.Errorf("%w: %d > allowed %d",
			ErrBlockTooFarInFuture,
			*headerExtra.TimeMilliseconds,
			maxBlockTimeMillis,
		)
	}

	// Verify minimum block delay is enforced
	if err := verifyMinDelay(parent, header); err != nil {
		return err
	}

	return nil
}

// verifyMinDelay verifies that the minimum block delay is enforced based on the min delay excess.
func verifyMinDelay(parent *types.Header, header *types.Header) error {
	headerExtra := customtypes.GetHeaderExtra(header)
	parentExtra := customtypes.GetHeaderExtra(parent)

	parentMinDelayExcess := customtypes.GetHeaderExtra(parent).MinDelayExcess
	// Parent might not have a min delay excess if this is the first Granite block
	// in this case we cannot verify the min delay,
	// Otherwise parent should have been verified in VerifyMinDelayExcess
	if parentMinDelayExcess == nil {
		return nil
	}

	// if parent has no TimeMilliseconds, no min delay is required
	// This should not happen as it should have been verified for Granite in VerifyTime.
	if parentExtra.TimeMilliseconds == nil {
		return nil
	}

	// This should not be underflow as we have verified that the parent's
	// TimeMilliseconds is earlier than the header's TimeMilliseconds in VerifyTime.
	actualDelayMillis := *headerExtra.TimeMilliseconds - *parentExtra.TimeMilliseconds

	minRequiredDelayMillis := parentMinDelayExcess.Delay()

	if actualDelayMillis < minRequiredDelayMillis {
		return fmt.Errorf("%w: actual delay %dms < required %dms",
			ErrMinDelayNotMet,
			actualDelayMillis,
			minRequiredDelayMillis,
		)
	}

	return nil
}
