// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"sync"
	"testing"
	"time"
)

var _ Tx = (*testTx)(nil)

type testTx struct {
	hash []byte
}

func (t *testTx) Hash() []byte {
	return t.hash
}

func (t *testTx) Marshal() ([]byte, error) {
	return t.hash, nil
}

func (t *testTx) Unmarshal(b []byte) error {
	t.hash = b
	return nil
}

func TestGossiper_Shutdown(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := &sync.WaitGroup{}
	puller := NewPullGossiper[*testTx](
		nil,
		nil,
		nil,
		time.Hour,
		0,
		shutdownChan,
		wg,
	)

	wg.Add(1)
	go puller.Start()

	close(shutdownChan)
	wg.Wait()
}
