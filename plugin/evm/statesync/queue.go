// (c) 2019-2020, Ava Labs, Inc.
//
// This file is although named queue.go, is not the same as the eth/downloader/queue.go file.
// Instead, this is simply the protected array for downloader (yeah not even a queue).
// It might be helpful to keep this in case we move computation here
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.

package statesync

import (
	"fmt"
	"sync"
)

type Executable interface {
	ExitQueue() error
}

type Queue[K Executable] struct {
	buffer  []*K
	l       sync.RWMutex
	nextPos int
	compare func(*K, *K) int
	closed  bool
}

func NewQueue[K Executable](size int, compare func(*K, *K) int) *Queue[K] {
	return &Queue[K]{
		buffer:  make([]*K, size),
		compare: compare,
	}
}

func (q *Queue[K]) Insert(h *K) error {
	q.l.Lock()
	defer q.l.Unlock()

	if q.nextPos >= len(q.buffer) {
		return fmt.Errorf("queue is full, cannot insert")
	}

	q.buffer[q.nextPos] = h
	q.nextPos++

	return nil
}

func (q *Queue[K]) Flush(max *K, close bool) error {
	q.l.Lock()
	defer q.l.Unlock()

	newBuffer := make([]*K, len(q.buffer))
	newPos := 0

	for i := 0; i < q.nextPos; i++ {
		// If the item is greater than max, postpone
		elem := q.buffer[i]
		if max != nil && q.compare(elem, max) < 0 {
			newBuffer[newPos] = q.buffer[i]
			newPos++
		} else {
			if err := (*elem).ExitQueue(); err != nil {
				return fmt.Errorf("error executing item: %w", err)
			}
		}
	}

	q.buffer = newBuffer
	q.nextPos = newPos

	if close {
		q.closed = true
	}

	return nil
}

func (q *Queue[K]) Len() int {
	q.l.RLock()
	defer q.l.RUnlock()

	return q.nextPos
}

func (q *Queue[K]) Closed() bool {
	q.l.RLock()
	defer q.l.RUnlock()

	return q.closed
}
