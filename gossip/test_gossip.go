// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"sync"
)

var (
	_ Gossipable   = (*testTx)(nil)
	_ Set[*testTx] = (*testMempool)(nil)
)

type testTx struct {
	hash Hash
}

func (t *testTx) GetHash() Hash {
	return t.hash
}

func (t *testTx) Marshal() ([]byte, error) {
	// TODO implement me
	panic("implement me")
}

func (t *testTx) Unmarshal(bytes []byte) error {
	// TODO implement me
	panic("implement me")
}

type testMempool struct {
	mempool []*testTx
	lock    sync.Mutex
}

func (t *testMempool) Add(tx *testTx) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.mempool = append(t.mempool, tx)
	return nil
}

func (t *testMempool) Get(filter func(tx *testTx) bool) []*testTx {
	t.lock.Lock()
	defer t.lock.Unlock()

	result := make([]*testTx, 0)
	for _, tx := range t.mempool {
		if !filter(tx) {
			continue
		}
		result = append(result, tx)
	}

	return result
}

func (t *testMempool) GetBloomFilter() *BloomFilter {
	t.lock.Lock()
	defer t.lock.Unlock()

	bloom, _ := NewBloomFilter(1000, 0.01)
	for _, tx := range t.mempool {
		bloom.Add(tx)
	}

	return bloom
}
