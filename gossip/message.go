// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

type PullTxsRequest struct {
	BloomFilter []byte `serialize:"true"`
}

type PullTxsResponse struct {
	Txs [][]byte `serialize:"true"`
}
