// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "time"

// ExtractMilliseconds returns the fractional milliseconds (0–999) of t within its second.
func ExtractMilliseconds(t time.Time) uint16 {
	return uint16(t.Nanosecond() / int(time.Millisecond))
}
