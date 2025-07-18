// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
)

func TestStateObjectPartition(t *testing.T) {
	h1 := common.Hash{}
	normalizeCoinID(&h1)

	h2 := common.Hash{}
	normalizeStateKey(&h2)
	if bytes.Equal(h1.Bytes(), h2.Bytes()) {
		t.Fatalf("Expected normalized hashes to be unique")
	}

	h3 := common.Hash([32]byte{0xff})
	normalizeCoinID(&h3)

	h4 := common.Hash([32]byte{0xff})
	normalizeStateKey(&h4)
	if bytes.Equal(h3.Bytes(), h4.Bytes()) {
		t.Fatal("Expected normalized hashes to be unqiue")
	}
}
