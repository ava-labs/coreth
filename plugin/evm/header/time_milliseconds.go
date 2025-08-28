// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/core/types"
)

var (
	ErrTimeMillisecondsRequired      = errors.New("TimeMillisecondsPart is required after Granite activation")
	ErrTimeMillisecondsInvalid       = errors.New("TimeMillisecondsPart must be between 0-999")
	ErrTimeMillisecondsBeforeGranite = errors.New("TimeMillisecondsPart should be nil before Granite activation")
)

func TimeMillisecondsPart(rulesExtra extras.AvalancheRules, tstart time.Time) *uint64 {
	if !rulesExtra.IsGranite {
		return nil
	}
	// Extract milliseconds portion from tstart
	timeMillisecondsPart := uint64(utils.ExtractMillisecondsTimestamp(tstart))
	return &timeMillisecondsPart
}

func VerifyTimeMilliseconds(rulesExtra extras.AvalancheRules, header *types.Header) error {
	headerExtra := customtypes.GetHeaderExtra(header)
	switch {
	case rulesExtra.IsGranite:
		// In syntacticVerify or verifyHeader
		if headerExtra.TimeMillisecondsPart == nil {
			return ErrTimeMillisecondsRequired
		}
		if *headerExtra.TimeMillisecondsPart > 999 { // TODO: should be removed after changing the type to uint16
			return fmt.Errorf("%w: got %d", ErrTimeMillisecondsInvalid, *headerExtra.TimeMillisecondsPart)
		}
	default:
		// Before Granite, TimeMillisecondsPart should be nil
		if headerExtra.TimeMillisecondsPart != nil {
			return ErrTimeMillisecondsBeforeGranite
		}
	}
	return nil
}
