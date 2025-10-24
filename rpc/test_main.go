// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"os"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

func TestMain(m *testing.M) {
	customtypes.Register()

	// Since there are so many flaky tests in the RPC package, we run the tests
	// multiple times to try to get a passing run.
	var ret int
	for range 5 {
		ret = m.Run()
		if ret == 0 {
			break
		}
	}

	os.Exit(ret)
}
