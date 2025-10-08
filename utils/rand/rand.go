// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rand

import (
	"crypto/rand"
	"math/big"
)

// Credit to Brandur Leach (@Brandur) for this implementation.
// https://brandur.org/fragments/crypto-rand-float64

// Intn is a shortcut for generating a random integer between 0 and
// n using crypto/rand.
func Intn(n int64) int64 {
	nBig, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic(err)
	}
	return nBig.Int64()
}

// SecureFloat64 is a shortcut for generating a random float between 0 and
// 1 using crypto/rand.
func SecureFloat64() float64 {
	return float64(Intn(1<<53)) / (1 << 53)
}
