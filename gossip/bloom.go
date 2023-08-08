// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"encoding/binary"
	"hash"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"
	"golang.org/x/exp/rand"
)

var _ hash.Hash64 = (*hasher)(nil)

func NewBloomFilter(m uint64, p float64) (*BloomFilter, error) {
	bloom, err := bloomfilter.NewOptimal(m, p)
	if err != nil {
		return nil, err
	}

	bloomFilter := &BloomFilter{
		Bloom: bloom,
		Salt:  randomSalt(),
	}
	return bloomFilter, nil
}

type BloomFilter struct {
	Bloom *bloomfilter.Filter
	Salt  []byte
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	salted := hasher{
		hash: gossipable.GetHash(),
		salt: b.Salt,
	}
	b.Bloom.Add(salted)
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	salted := hasher{
		hash: gossipable.GetHash(),
		salt: b.Salt,
	}
	return b.Bloom.Contains(salted)
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
	bloomFilter.Salt = randomSalt()
	return true
}

func randomSalt() []byte {
	salt := make([]byte, 0, HashLength)
	r := rand.New(rand.NewSource(uint64(time.Now().Nanosecond())))
	_, _ = r.Read(salt)
	return salt
}

type hasher struct {
	hash.Hash64
	hash Hash
	salt []byte
}

func (h hasher) Sum64() uint64 {
	for i, salt := range h.salt {
		h.hash[i] ^= salt
	}

	return binary.BigEndian.Uint64(h.hash[:])
}

func (h hasher) Size() int {
	return HashLength
}
