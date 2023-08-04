// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

type Filter interface {
	Add(gossipable Gossipable)
	Has(gossipable Gossipable) bool
}
