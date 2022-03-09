// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/peer"
)

type testSender struct {
	lock              sync.Mutex
	t                 *testing.T
	timeout           time.Duration
	activeRequests    map[uint32]struct{}
	network           peer.Network
	sendAppRequestFn  func(ids.ShortSet, uint32, []byte) error
	sendAppResponseFn func(ids.ShortID, uint32, []byte) error
}

func newTestSender(t *testing.T, timeout time.Duration, sendAppRequestFn func(ids.ShortSet, uint32, []byte) error, sendAppResponseFn func(ids.ShortID, uint32, []byte) error) *testSender {
	return &testSender{t: t, timeout: timeout, sendAppRequestFn: sendAppRequestFn, sendAppResponseFn: sendAppResponseFn, activeRequests: make(map[uint32]struct{})}
}

func (t *testSender) SendAppGossipSpecific(ids.ShortSet, []byte) error {
	panic("not implemented")
}

func (t *testSender) SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, appRequestBytes []byte) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.activeRequests[requestID] = struct{}{}
	nodeID, _ := nodeIDs.Peek()
	go func(delay time.Duration, nodeID ids.ShortID) {
		time.Sleep(delay)
		// check if this request is unfulfilled, if so, fire AppRequestFailed
		t.lock.Lock()
		defer t.lock.Unlock()
		if _, exists := t.activeRequests[requestID]; exists {
			delete(t.activeRequests, requestID)
			if err := t.network.AppRequestFailed(nodeID, requestID); err != nil {
				t.t.Fatal("unexpected error when firing AppRequestFailed", err)
			}
		}
	}(t.timeout, nodeID)
	return t.sendAppRequestFn(nodeIDs, requestID, appRequestBytes)
}

func (t *testSender) SendAppResponse(nodeID ids.ShortID, requestID uint32, appResponseBytes []byte) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if _, exists := t.activeRequests[requestID]; !exists {
		return fmt.Errorf("cannot send response to already fulfilled request")
	}
	delete(t.activeRequests, requestID)
	return t.sendAppResponseFn(nodeID, requestID, appResponseBytes)
}

func (t *testSender) SendAppGossip([]byte) error {
	panic("implement me")
}
