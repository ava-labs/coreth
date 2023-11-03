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
package state

import (
	"sync"
)

type BoundedWorkers struct {
	numWorkers int
	wg         sync.WaitGroup
	work       chan func()
	workers    chan struct{}
}

// NewBoundedWorkers returns a new work group that creates a maximum of [numWorkers]
// on demand to execute functions.
func NewBoundedWorkers(numWorkers int) *BoundedWorkers {
	return &BoundedWorkers{
		numWorkers: numWorkers,
		work:       make(chan func()),
		workers:    make(chan struct{}, numWorkers),
	}
}

// startWorker creates a new goroutines to execute f immediately and then keep the goroutine
// alive to continue executing new work.
func (b *BoundedWorkers) startWorker(f func()) {
	b.wg.Add(1)

	go func() {
		defer b.wg.Done()

		if f != nil {
			f()
		}
		for f := range b.work {
			f()
		}
	}()
}

func (b *BoundedWorkers) NumWorkers() int { return b.numWorkers }

// Execute the given function on an existing goroutine waiting for more work, a new goroutine,
// or synchronously on this thread.
//
// The caller must ensure that it correctly handles any of these cases.
func (b *BoundedWorkers) Execute(f func()) {
	select {
	case b.work <- f: // Feed hungry workers first.
	case b.workers <- struct{}{}: // Allocate a new worker to execute immediately next.
		b.startWorker(f)
	default: // Hit the parallelism cap? Execute immediately.
		f()
	}
}

// Stop closes the group and waits for all goroutines to exit.
//
// Must not call Execute after calling Stop.
// It is not safe to call Stop multiple times.
func (b *BoundedWorkers) Stop() {
	close(b.work)
	b.wg.Wait()
}
