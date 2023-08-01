// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"sync"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Gossipable   = (*testTx)(nil)
	_ Set[*testTx] = (*testMempool)(nil)
)

type testTx struct {
	id ids.ID
}

func (t *testTx) GetID() ids.ID {
	return t.id
}

func (t *testTx) Marshal() ([]byte, error) {
	return t.id[:], nil
}

func (t *testTx) Unmarshal(b []byte) error {
	copy(t.id[:], b)
	return nil
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

func (t *testMempool) GetBloomFilter() ([]byte, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	bloom, err := bloomfilter.New(DefaultBloomM, DefaultBloomK)
	if err != nil {
		return nil, err
	}

	for _, tx := range t.mempool {
		bloom.Add(NewHasher(tx.GetID()))
	}

	return bloom.MarshalBinary()
}
