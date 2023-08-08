// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"sync"
	"testing"
	"time"
)

func TestGossiperShutdown(t *testing.T) {
	config := Config{Frequency: time.Second}
	puller := NewGossiper[testTx, *testTx](config, nil, nil, nil, 0)
	done := make(chan struct{})
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go puller.Gossip(done, wg)

	close(done)
	wg.Wait()
}
