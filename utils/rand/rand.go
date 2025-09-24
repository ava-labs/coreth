// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rand

import (
	"crypto/rand"
	"encoding/binary"
	"math"
)

func SecureFloat64() float64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	bits := binary.BigEndian.Uint64(b[:])
	bits = (bits >> 12) | (1023 << 52) // 52-bit mantissa, exponent=1023 -> [1,2)
	return math.Float64frombits(bits) - 1.0
}
