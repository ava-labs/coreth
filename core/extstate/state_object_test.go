// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
)

func TestStateObjectPartition(t *testing.T) {
	keys := []common.Hash{
		{},
		{0xff},
	}
	for _, key := range keys {
		t.Run(key.Hex(), func(t *testing.T) {
			var (
				coin  = key
				state = key
			)
			normalizeCoinID(&coin)
			normalizeStateKey(&state)
			assert.NotEqual(t, coin, state, "Expected normalized hashes to be unique")
		})
	}
}
