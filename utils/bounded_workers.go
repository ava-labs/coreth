// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sync"
	"sync/atomic"
)

type BoundedWorkers struct {
	wg          sync.WaitGroup
	workerCount atomic.Int32
	work        chan func()
	workers     chan struct{}
}

// NewBoundedWorkers returns a new work group that creates a maximum of [numWorkers],
// as needed.
func NewBoundedWorkers(numWorkers int) *BoundedWorkers {
	return &BoundedWorkers{
		work:    make(chan func()),
		workers: make(chan struct{}, numWorkers),
	}
}

// startWorker creates a new goroutine to execute [f] immediately and then keeps the goroutine
// alive to continue executing new work.
func (b *BoundedWorkers) startWorker(f func()) {
	b.workerCount.Add(1)
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

// Execute the given function on an existing goroutine waiting for more work, a new goroutine,
// or return if there is activity on [cancel].
func (b *BoundedWorkers) Execute(cancel chan struct{}, f func()) bool {
	select {
	case b.work <- f: // Feed hungry workers first.
		return true
	case b.workers <- struct{}{}: // Allocate a new worker to execute immediately next.
		b.startWorker(f)
		return true
	case <-cancel:
		return false
	}
}

// Stop closes the group and waits for all goroutines to exit. Stop
// returns the number of workers that were spawned during the run.
//
// Execute must not be called after calling Stop.
// It is not safe to call Stop multiple times.
func (b *BoundedWorkers) Stop() int {
	close(b.work)
	b.wg.Wait()
	return int(b.workerCount.Load())
}
