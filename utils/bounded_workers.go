// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sync"
	"sync/atomic"
)

type BoundedWorkers struct {
	workerSpawner      chan struct{}
	workerCount        atomic.Int32
	outstandingWorkers sync.WaitGroup

	outstandingWork sync.WaitGroup

	work      chan func()
	workClose sync.Once
}

// NewBoundedWorkers returns an instance of [BoundedWorkers] that
// will spawn up to [max] goroutines.
func NewBoundedWorkers(max int) *BoundedWorkers {
	return &BoundedWorkers{
		work:          make(chan func()),
		workerSpawner: make(chan struct{}, max),
	}
}

// startWorker creates a new goroutine to execute [f] immediately and then keeps the goroutine
// alive to continue executing new work.
func (b *BoundedWorkers) startWorker(f func()) {
	b.workerCount.Add(1)
	b.outstandingWorkers.Add(1)

	go func() {
		defer b.outstandingWorkers.Done()

		if f != nil {
			f()
			b.outstandingWork.Done()
		}
		for f := range b.work {
			f()
			b.outstandingWork.Done()
		}
	}()
}

// Execute the given function on an existing goroutine waiting for more work, a new goroutine,
// or return if the context is canceled.
//
// Execute must not be called after Wait, otherwise it might panic.
func (b *BoundedWorkers) Execute(f func()) {
	b.outstandingWork.Add(1)

	select {
	case b.work <- f: // Feed hungry workers first.
	case b.workerSpawner <- struct{}{}: // Allocate a new worker to execute immediately next.
		b.startWorker(f)
	}
}

// Wait returns after all enqueued work finishes and all goroutines to exit.
// Wait returns the number of workers that were spawned during the run.
//
// It is safe to call Wait multiple times but not safe to call [Execute]
// after [Wait] has been called.
func (b *BoundedWorkers) Wait() int {
	b.outstandingWork.Wait()
	b.workClose.Do(func() {
		close(b.work)
	})
	b.outstandingWorkers.Wait()
	return int(b.workerCount.Load())
}
