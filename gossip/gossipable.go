// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import "github.com/ava-labs/avalanchego/ids"

// Gossipable is an item that can be gossiped across the network
type Gossipable interface {
	// GetID represents the unique id of this item
	GetID() ids.ID
	// Marshal returns the byte representation of this item
	Marshal() ([]byte, error)
	// Unmarshal deserializes the provided bytes in-place
	Unmarshal(b []byte) error
}

// Set holds a set of known Gossipable items
type Set[T Gossipable] interface {
	// Add adds a Gossipable to the set
	Add(gossipable T) (bool, error)
	// Get returns elements that match the provided filter function
	Get(filter func(gossipable T) bool) []T
	// GetBloomFilter returns a bloom filter representing the items in Set.
	GetBloomFilter() ([]byte, error)
}
