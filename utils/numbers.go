// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"strconv"
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

// Uint64PtrEqual returns true if x and y pointers are equivalent ie. both nil or both
// contain the same value.
func Uint64PtrEqual(x, y *uint64) bool {
	if x == nil || y == nil {
		return x == y
	}
	return *x == *y
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

// SafeDerefUint64String safely dereferences a uint64 pointer and returns a string.
func SafeDerefUint64String(ptr *uint64) string {
	if ptr == nil {
		return "nil"
	}
	return strconv.FormatUint(*ptr, 10)
}

func Uint64PtrFrom[T ~uint64](v *T) *uint64 {
	if v == nil {
		return nil
	}
	u := uint64(*v)
	return &u
}

func Uint64PtrTo[T ~uint64](v *uint64) *T {
	if v == nil {
		return nil
	}
	t := T(*v)
	return &t
}
