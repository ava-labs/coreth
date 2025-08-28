// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "time"

// ExtractMillisecondsTimestamp returns the fractional milliseconds (0–999) of t within its second,
// extracted from the Unix milliseconds timestamp.
func ExtractMillisecondsTimestamp(t time.Time) uint16 {
	return uint16(t.UnixMilli() % 1000)
}
