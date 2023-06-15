// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"encoding/binary"
	"hash"
	"sync"

	bloomfilter "github.com/holiman/bloomfilter/v2"
)

func NewBloomFilterFromBytes(bytes []byte) (*BloomFilter, error) {
	b, err := NewBloomFilter()
	if err != nil {
		return nil, err
	}

	return b, b.bloomFilter.UnmarshalBinary(bytes)
}

func NewBloomFilter() (*BloomFilter, error) {
	return newBloomFilter(1024, 4, 0.75)
}

func newBloomFilter(m, k uint64, refreshRatio float64) (*BloomFilter, error) {
	bloomFilter, err := bloomfilter.New(m, k)
	if err != nil {
		return nil, err
	}

	return &BloomFilter{
		bloomFilter:  bloomFilter,
		m:            m,
		k:            k,
		refreshRatio: refreshRatio,
	}, nil
}

// BloomFilter is a bloom filter that is periodically refreshed when it breaches a given
// threshold of items on Adds
type BloomFilter struct {
	m, k         uint64
	refreshRatio float64
	bloomFilter  *bloomfilter.Filter
	lock         sync.RWMutex
}

// Add invariant: requires that [hash] is at least 8 bytes long.
func (b *BloomFilter) Add(hash []byte) {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.resetIfNeeded()

	binary.BigEndian.Uint64(hash)
	b.bloomFilter.Add(bloomHasher{Hash: hash})
}

// assumes [b.lock] is held
func (b *BloomFilter) resetIfNeeded() {
	filled := b.bloomFilter.PreciseFilledRatio()
	if filled >= b.refreshRatio {
		b.bloomFilter, _ = bloomfilter.New(b.m, b.k)
	}
}

func (b *BloomFilter) Contains(hash []byte) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()

	return b.bloomFilter.Contains(bloomHasher{Hash: hash})
}

func (b *BloomFilter) MarshalBinary() ([]byte, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()

	return b.bloomFilter.MarshalBinary()
}

func (b *BloomFilter) UnmarshalBinary(bytes []byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	return b.bloomFilter.UnmarshalBinary(bytes)
}

var _ hash.Hash64 = (*bloomHasher)(nil)

type bloomHasher struct {
	hash.Hash64
	Hash []byte
}

func (b bloomHasher) Sum64() uint64 {
	return binary.BigEndian.Uint64(b.Hash)
}
