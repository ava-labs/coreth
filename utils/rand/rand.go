// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rand

import (
	"crypto/rand"
	"encoding/binary"
)

// SecureIntn generates a secure random integer in the range [0, n).
// If crypto/rand fails, it returns 0 as a fallback.
func SecureIntn(n int) int {
	if n <= 0 {
		return 0
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	return int(binary.BigEndian.Uint32(b)) % n
}

// SecureUint64 generates a secure random uint64.
// If crypto/rand fails, it returns 0 as a fallback.
func SecureUint64() uint64 {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// SecureFloat64 generates a secure random float64 in the range [0.0, 1.0).
// If crypto/rand fails, it returns 0.5 as a fallback.
func SecureFloat64() float64 {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return 0.5
	}
	// Convert bytes to uint64 and normalize to [0.0, 1.0)
	return float64(binary.BigEndian.Uint64(b)) / (1 << 64)
}

// SecureIntRange generates a secure random integer in the range [min, max).
// If crypto/rand fails, it returns min as a fallback.
func SecureIntRange(min, max int) int {
	if min >= max {
		return min
	}
	return min + SecureIntn(max-min)
}

// SecureByte generates a secure random byte.
// If crypto/rand fails, it returns 0 as a fallback.
func SecureByte() byte {
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	return b[0]
}
