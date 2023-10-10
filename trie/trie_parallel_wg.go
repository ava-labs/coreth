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
	"sync"
)

type BoundedWorkers struct {
	wg      sync.WaitGroup
	work    chan func()
	workers chan struct{}
}

func NewBoundedWorkers(numWorkers int) *BoundedWorkers {
	return &BoundedWorkers{
		work:    make(chan func()),
		workers: make(chan struct{}, numWorkers),
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

func (b *BoundedWorkers) Execute(f func()) {
	select {
	case b.work <- f: // Feed hungry workers first.
	case b.workers <- struct{}{}: // Allocate a new worker to execute immediately next.
		b.startWorker(f)
	default: // Hit the parallelism cap? Execute immediately.
		f()
	}
}

func (b *BoundedWorkers) Stop() {
	close(b.work)
	b.wg.Wait()
}
