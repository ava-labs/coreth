// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"encoding/binary"
	"hash"

	bloomfilter "github.com/holiman/bloomfilter/v2"
)

const (
	DefaultBloomM = 8 * 1024 // 8 KiB
	DefaultBloomK = 4
	// DefaultBloomMaxFilledRatio is the max ratio of filled slots in the bloom
	// filter before we reset it to avoid too many collisions.
	DefaultBloomMaxFilledRatio = 0.75
)

var (
	_ Filter      = (*BloomFilter)(nil)
	_ hash.Hash64 = (*hasher)(nil)
)

func NewDefaultBloomFilter() (*BloomFilter, error) {
	return NewBloomFilter(DefaultBloomM, DefaultBloomK)
}

func NewBloomFilter(m, k uint64) (*BloomFilter, error) {
	bloom, err := bloomfilter.New(m, k)
	if err != nil {
		return nil, err
	}

	return &BloomFilter{
		Bloom: bloom,
	}, nil
}

type BloomFilter struct {
	Bloom *bloomfilter.Filter `serialize:"true"`
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	b.Bloom.Add(NewHasher(gossipable.GetHash()))
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	return b.Bloom.Contains(NewHasher(gossipable.GetHash()))
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a ratio of
// filled elements. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	maxFilledRatio float64,
) bool {
	if bloomFilter.Bloom.PreciseFilledRatio() < maxFilledRatio {
		return false
	}

	// it's not possible for this to error assuming that the original
	// bloom filter's parameters were valid
	fresh, _ := bloomfilter.New(bloomFilter.Bloom.M(), bloomFilter.Bloom.K())
	bloomFilter.Bloom = fresh
	return true
}

func NewHasher(hash Hash) hash.Hash64 {
	return hasher{hash: hash}
}

type hasher struct {
	hash.Hash64
	hash Hash
}

func (h hasher) Sum64() uint64 {
	return binary.BigEndian.Uint64(h.hash[:])
}

func (h hasher) Size() int {
	return 8
}
