// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"encoding/binary"
	"hash"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	DefaultBloomM = 8 * 1024 // 8 KiB
	DefaultBloomK = 4
	// DefaultBloomMaxFilledRatio is the max ratio of filled slots in the bloom
	// filter before we reset it to avoid too many collisions.
	DefaultBloomMaxFilledRatio = 0.75
)

var _ hash.Hash64 = (*hasher)(nil)

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a ratio of
// filled elements. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter **bloomfilter.Filter,
	maxFilledRatio float64,
) bool {
	if (*bloomFilter).PreciseFilledRatio() < maxFilledRatio {
		return false
	}

	// it's not possible for this to error assuming that the original
	// bloom filter's parameters were valid
	fresh, _ := bloomfilter.New((*bloomFilter).M(), (*bloomFilter).K())
	*bloomFilter = fresh

	return true
}

func NewHasher(id ids.ID) hash.Hash64 {
	return hasher{ID: id}
}

type hasher struct {
	hash.Hash64
	ID ids.ID
}

func (h hasher) Sum64() uint64 {
	return binary.BigEndian.Uint64(h.ID[:])
}

func (h hasher) Size() int {
	return 8
}
