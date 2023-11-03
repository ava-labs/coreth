// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"sync"
	"sync/atomic"
)

type BoundedWorkers struct {
	workerSpawn        chan struct{}
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
		work:        make(chan func()),
		workerSpawn: make(chan struct{}, max),
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
// Execute must not be called after Stop.
func (b *BoundedWorkers) Execute(ctx context.Context, f func()) bool {
	b.outstandingWork.Add(1)
	select {
	case b.work <- f: // Feed hungry workers first.
		return true
	case b.workerSpawn <- struct{}{}: // Allocate a new worker to execute immediately next.
		b.startWorker(f)
		return true
	case <-ctx.Done():
		b.outstandingWork.Done()
		return false
	}
}

// Stop closes the group and waits for all enqueued work to finish and all goroutines to exit.
// Stop returns the number of workers that were spawned during the run.
//
// It is safe to call Stop multiple times.
func (b *BoundedWorkers) Stop() int {
	b.outstandingWork.Wait()
	b.workClose.Do(func() {
		close(b.work)
	})
	b.outstandingWorkers.Wait()
	return int(b.workerCount.Load())
}
