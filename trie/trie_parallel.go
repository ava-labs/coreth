// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package trie implements Merkle Patricia Tries.
package trie

import (
	"encoding/binary"
	"fmt"
	"sync"
)

// newInternalTrieNode constructs a trie node containing the specified key-value pairs.
// The node is constructed entirely in-memory without hashing, so the returned node and
// its children are guaranteed not to contain any hashNodes.
func newInternalTrieNode(keys [][]byte, values [][]byte) (node, error) {
	if len(keys) != len(values) {
		return nil, fmt.Errorf("cannot create internal trie with mismatch numKeys (%d) != numValues (%d)", len(keys), len(values))
	}

	tr := &Trie{root: nil, reader: newEmptyReader(), tracer: newTracer()}
	for i, key := range keys {
		if err := tr.update(key, values[i]); err != nil {
			return nil, err
		}
	}

	return tr.root, nil
}

func (t *Trie) BatchGet(maxWorkers int, keys [][]byte) error {
	// Create values containing the indices of the respective keys
	values := make([][]byte, len(keys))
	for i := 0; i < len(keys); i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		values[i] = key
	}
	applyNode, err := newInternalTrieNode(keys, values)
	if err != nil {
		return err
	}

	bw := NewBoundedWorkers(maxWorkers)
	defer bw.Stop()

	newnode, didResolve, err := t.applyGet(bw, t.root, applyNode, make([]byte, 0), 0)
	if err == nil && didResolve {
		t.root = newnode
	}
	return err
}

// recurse traverses the root node and apply nodes to find the node in the children of root that has the longest shared prefix with any node in apply.
// Once it finds the child in root with such a prefix, it applies the function f with the arguments:
// rootNodePrefix - the prefix leading up to the nearest neighbor in root
// rootNodeNeighbor - the node in root with such a prefix
// applyPrefix - the prefix up to applyNode
// applyNode - a leaf node in apply
// TODO: try this approach
// func (t *Trie) recurse(root node, apply node, f func(rootNodePrefix []byte, rootNodeNeighbor node, applyPrefix []byte, applyNode node) error) error {
// 	return nil
// }

// what do I want
// traverse the node and its children to all of h

// commonPrefix is the shared prefix up to to the root of origNode and potentially partway through the extension key if origNode is a shortNode
// - can we get rid of the possibility they diverge?
// pos is the length of the path from the root to the base of origNode
// a shortNode never has a Key of length 0
func (t *Trie) applyGet(bw *BoundedWorkers, origNode node, applyNode node, commonPrefix []byte, pos int) (newnode node, didResolve bool, err error) {
	switch n := origNode.(type) {
	case nil:
		return nil, false, nil
	case valueNode:
		return n, false, nil
	case *shortNode:
		return t.applyGetShortNode(bw, n, applyNode, commonPrefix, pos)
	case *fullNode:
		return t.applyGetFullNode(bw, n, applyNode, commonPrefix, pos)
	case hashNode:
		child, err := t.resolveAndTrack(n, commonPrefix)
		if err != nil {
			return n, true, err
		}
		newnode, _, err := t.applyGet(bw, child, applyNode, commonPrefix, pos)
		return newnode, true, err
	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

func (t *Trie) applyGetShortNode(bw *BoundedWorkers, n *shortNode, origApplyNode node, commonPrefix []byte, pos int) (newnode node, didResolve bool, err error) {
	switch applyNode := origApplyNode.(type) {
	case nil:
		return n, false, nil
	case valueNode:
		return n, false, nil
	case *shortNode:
		nKeyIndex := len(commonPrefix) - pos // XXX
		nKeySegment := n.Key[nKeyIndex:]     // nKeySegment could have length 0
		extendedPrefixLen := prefixLen(nKeySegment, applyNode.Key)

		// If the shared prefix doesn't exhaust either key, then they diverge and we can
		// throw away remaining get requests
		if extendedPrefixLen < len(nKeySegment) && extendedPrefixLen < len(applyNode.Key) {
			return n, false, nil
		}

		// extendedPrefixLen exhausts either nKeySegment or applyNode.Key
		newCommonPrefix := make([]byte, len(commonPrefix)+extendedPrefixLen)
		copy(newCommonPrefix, commonPrefix)
		copy(newCommonPrefix[len(commonPrefix):], nKeySegment[:extendedPrefixLen])

		// Does the below switch assume nKeySegment has length > 0? No, it will hit the first case and jump to use Val
		switch {
		case extendedPrefixLen < len(applyNode.Key): // => extendedPrefixLen == len(nKeySegment)
			// The updated shortNode must have Key length > 1 since extendedPrefixLen < len(applyNode.Key)
			applyNode = applyNode.copy() // Copy should be unnecessary here since we own the applyNode within this function
			applyNode.Key = applyNode.Key[extendedPrefixLen:]
			newnode, didResolve, err := t.applyGet(bw, n.Val, applyNode, newCommonPrefix, pos+extendedPrefixLen)
			if err == nil && didResolve {
				n = n.copy()
				n.Val = newnode
			}
			return n, didResolve, err
		case extendedPrefixLen < len(nKeySegment): // => extendedPrefixLen == len(applyNode.Key)
			return t.applyGetShortNode(bw, n, applyNode.Val, newCommonPrefix, pos)
		default: // extendedPrefixLen == len(applyNode.Key) == len(nKeySegment)
			newnode, didResolve, err := t.applyGet(bw, n.Val, applyNode.Val, newCommonPrefix, pos+len(n.Key))
			if err == nil && didResolve {
				n = n.copy()
				n.Val = newnode
			}
			return n, didResolve, err
		}
	case *fullNode:
		nKeyIndex := len(commonPrefix) - pos
		if nKeyIndex == len(n.Key) {
			newnode, didResolve, err := t.applyGet(bw, n.Val, applyNode, commonPrefix, pos+len(n.Key))
			if err == nil && didResolve {
				n = n.copy()
				n.Val = newnode
			}
			return n, didResolve, err
		}
		nKeyNibble := n.Key[nKeyIndex]

		newCommonPrefix := make([]byte, len(commonPrefix)+1)
		copy(newCommonPrefix, commonPrefix)
		newCommonPrefix[len(commonPrefix)] = nKeyNibble

		return t.applyGetShortNode(bw, n, applyNode.Children[nKeyNibble], newCommonPrefix, pos)
	default: // Note: hashNode is not allowed in the applyNode
		panic(fmt.Sprintf("%T: invalid node: %v", origApplyNode, origApplyNode))
	}
}

func (t *Trie) applyGetFullNode(bw *BoundedWorkers, n *fullNode, origApplyNode node, commonPrefix []byte, pos int) (newnode node, didResolve bool, err error) {
	switch applyNode := origApplyNode.(type) {
	case nil:
		return n, false, nil
	case valueNode:
		return n, false, nil
	case *shortNode:
		nibble := applyNode.Key[0]
		child := n.Children[nibble]
		if len(applyNode.Key) > 1 { // XXX same reference
			applyNode.Key = applyNode.Key[1:]
		} else {
			origApplyNode = applyNode.Val
		}
		newCommonPrefix := make([]byte, len(commonPrefix)+1)
		copy(newCommonPrefix, commonPrefix)
		newCommonPrefix[len(commonPrefix)] = nibble
		newnode, didResolve, err := t.applyGet(bw, child, origApplyNode, newCommonPrefix, pos+1)
		if err == nil && didResolve {
			n = n.copy()
			n.Children[nibble] = newnode
		}
		return n, didResolve, err
	case *fullNode:
		var (
			resolved bool
			gErr     error
			wg       sync.WaitGroup
			lock     sync.Mutex
		)
		for nibble, nChild := range n.Children[:16] {
			if nChild == nil {
				continue
			}

			applyChild := applyNode.Children[nibble]
			if applyChild == nil {
				continue
			}

			nibble, nChild := nibble, nChild
			wg.Add(1)
			bw.Execute(func() {
				defer wg.Done()

				newCommonPrefix := make([]byte, len(commonPrefix)+1)
				copy(newCommonPrefix, commonPrefix)
				newCommonPrefix[len(commonPrefix)] = byte(nibble)
				newnode, didResolve, err := t.applyGet(bw, nChild, applyChild, newCommonPrefix, pos+1)

				lock.Lock()
				defer lock.Unlock()

				if err != nil {
					gErr = err
					return
				}

				if didResolve {
					resolved = true
					if !resolved {
						n = n.copy()
						resolved = true
					}
					n.Children[nibble] = newnode
				}
			})
		}

		wg.Wait()
		return n, resolved, gErr
	default: // Note: hashNode is not allowed in the applyNode
		panic(fmt.Sprintf("%T: invalid node: %v", origApplyNode, origApplyNode))
	}
}
