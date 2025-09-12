// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rand

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"math/big"
)

// SecureIntn returns uniform in [0, n). For n <= 0, returns 0.
func SecureIntn(n int) int {
	if n <= 0 {
		return 0
	}
	bn, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return int(bn.Int64())
}

func SecureFloat64() float64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	bits := binary.BigEndian.Uint64(b[:])
	bits = (bits >> 12) | (1023 << 52) // 52-bit mantissa, exponent=1023 -> [1,2)
	return math.Float64frombits(bits) - 1.0
}

// SecureIntRange returns uniform in [min, max).
// Panics if min >= max or on crypto/rand failure.
func SecureIntRange(min, max int) int {
	if min >= max {
		panic("invalid range: min >= max")
	}
	// (max - min) must be representable in int64 for big.NewInt; this is safe on 64-bit.
	width := max - min
	return min + SecureIntn(width)
}
