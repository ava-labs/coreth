// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

var _ Gossipable = (*testTx)(nil)

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
