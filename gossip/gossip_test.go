// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"sync"
	"testing"
	"time"
)

func TestPullGossiperShutdown(t *testing.T) {
	puller := NewGossiper[testTx, *testTx](
		nil,
		nil,
		nil,
		0,
		0,
		time.Hour,
	)
	done := make(chan struct{})
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go puller.Pull(done, wg)

	close(done)
	wg.Wait()
}
