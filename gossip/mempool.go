// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

type Tx interface {
	Hash() []byte
	Marshal() ([]byte, error)
	Unmarshal(b []byte) error
}

type Mempool[T Tx] interface {
	AddTx(tx T) error
	GetPendingTxs() []T
	GetPendingTxsBloomFilter() *BloomFilter
}
