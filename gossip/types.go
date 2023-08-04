// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

const HashLength = 8

type Hash [HashLength]byte

func HashFromBytes(b []byte) Hash {
	h := Hash{}
	copy(h[:], b)
	return h
}
