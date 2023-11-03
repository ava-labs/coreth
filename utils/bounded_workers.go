// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"sync"
	"sync/atomic"
)

type BoundedWorkers struct {
	doneLock sync.RWMutex
	done     bool

	wg          sync.WaitGroup
	workerCount atomic.Int32
	work        chan func()
	workClose   sync.Once
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
// or return if the context is canceled.
//
// If Execute is called after Stop, this function will return false.
func (b *BoundedWorkers) Execute(ctx context.Context, f func()) bool {
	// We use an RLock here to ensure Stop cannont be called while we are waiting for
	// our work to be enqueued (would cause a panic).
	b.doneLock.RLock()
	defer b.doneLock.RUnlock()

	// Check if we can enqueue work
	if b.done {
		return false
	}

	select {
	case b.work <- f: // Feed hungry workers first.
		return true
	case b.workers <- struct{}{}: // Allocate a new worker to execute immediately next.
		b.startWorker(f)
		return true
	case <-ctx.Done():
		return false
	}
}

// Stop closes the group and waits for all goroutines to exit. Stop
// returns the number of workers that were spawned during the run.
//
// It is safe to call Stop multiple times.
func (b *BoundedWorkers) Stop() int {
	// Once we attempt to grab this Lock, no more RLocks will be issued
	// during Execute. This allows for a graceful halt even if there
	// are blocked callers of Execute.
	b.doneLock.Lock()
	defer b.doneLock.Unlock()

	b.done = true
	b.workClose.Do(func() {
		close(b.work)
	})
	b.wg.Wait()
	return int(b.workerCount.Load())
}
