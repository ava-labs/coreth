// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"time"
)

func NewUint64(val uint64) *uint64 { return &val }

func TimeToNewUint64(time time.Time) *uint64 {
	unix := uint64(time.Unix())
	return NewUint64(unix)
}

func Uint64ToTime(val *uint64) time.Time {
	timestamp := int64(*val)
	return time.Unix(timestamp, 0)
}

// PointerEqual returns true if x and y point to the same values, or are both
// nil.
func PointerEqual[T comparable](x, y *T) bool {
	if x == nil || y == nil {
		return x == y
	}
	return *x == *y
}

// PointerEqualsValue returns true if p points to a value equal to v.
func PointerEqualsValue[T comparable](p *T, v T) bool {
	return p != nil && *p == v
}

// BigEqual returns true if a is equal to b. If a and b are nil, it returns
// true.
func BigEqual(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}

// BigEqualUint64 returns true if a is equal to b. If a is nil or not a uint64,
// it returns false.
func BigEqualUint64(a *big.Int, b uint64) bool {
	return a != nil &&
		a.IsUint64() &&
		a.Uint64() == b
}

// BigLessOrEqualUint64 returns true if a is less than or equal to b. If a is
// nil or not a uint64, it returns false.
func BigLessOrEqualUint64(a *big.Int, b uint64) bool {
	return a != nil &&
		a.IsUint64() &&
		a.Uint64() <= b
}
