// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/avalanchego/version"
)

type testClient struct {
	// captured request data
	numCalls         uint
	requestedVersion version.Application
	request          []byte

	// response mocking for RequestAny and Request calls
	response       [][]byte
	requestErr     []error
	nodesRequested []ids.ShortID
}

func (t *testClient) RequestAny(minVersion version.Application, request []byte) ([]byte, ids.ShortID, error) {
	if len(t.response) == 0 {
		return nil, ids.ShortEmpty, errors.New("no mocked response to return in testClient")
	}

	t.requestedVersion = minVersion

	response, err := t.processMock(request)
	return response, ids.ShortEmpty, err
}

func (t *testClient) Request(nodeID ids.ShortID, request []byte) ([]byte, error) {
	if len(t.response) == 0 {
		return nil, errors.New("no mocked response to return in testClient")
	}

	t.nodesRequested = append(t.nodesRequested, nodeID)

	return t.processMock(request)
}

func (t *testClient) processMock(request []byte) ([]byte, error) {
	t.request = request
	t.numCalls++

	response := t.response[0]
	if len(t.response) > 0 {
		t.response = t.response[1:]
	} else {
		t.response = nil
	}

	var err error
	if len(t.requestErr) > 0 {
		err = t.requestErr[0]
		t.requestErr = t.requestErr[1:]
	}

	return response, err
}

func (t *testClient) Gossip([]byte) error {
	panic("not implemented") // we don't care about this function for this test
}

func (t *testClient) mockResponse(times uint8, response []byte) {
	t.response = make([][]byte, times)
	for i := uint8(0); i < times; i++ {
		t.response[i] = response
	}
	t.numCalls = 0
}

func (t *testClient) mockResponses(responses ...[]byte) {
	t.response = responses
	t.numCalls = 0
}
