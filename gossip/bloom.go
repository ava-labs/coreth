// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"encoding/binary"
	"hash"

	bloomfilter "github.com/holiman/bloomfilter/v2"
)

var _ hash.Hash64 = (*hasher)(nil)

func NewBloomFilter(m uint64, p float64) (*BloomFilter, error) {
	bloom, err := bloomfilter.NewOptimal(m, p)
	if err != nil {
		return nil, err
	}

	return &BloomFilter{
		bloom: bloom,
	}, nil
}

type BloomFilter struct {
	bloom *bloomfilter.Filter
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	b.bloom.Add(NewHasher(gossipable))
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	return b.bloom.Contains(NewHasher(gossipable))
}

func (b *BloomFilter) Marshal() ([]byte, error) {
	return b.bloom.MarshalBinary()
}

func (b *BloomFilter) Unmarshal(data []byte) error {
	bloom := &bloomfilter.Filter{}
	err := bloom.UnmarshalBinary(data)
	b.bloom = bloom
	return err
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a ratio of
// filled elements. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	maxFilledRatio float64,
) bool {
	if bloomFilter.bloom.PreciseFilledRatio() < maxFilledRatio {
		return false
	}

	// it's not possible for this to error assuming that the original
	// bloom filter's parameters were valid
	fresh, _ := bloomfilter.New(bloomFilter.bloom.M(), bloomFilter.bloom.K())
	bloomFilter.bloom = fresh
	return true
}

func NewHasher(gossipable Gossipable) hash.Hash64 {
	return hasher{hash: gossipable.GetHash()}
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
