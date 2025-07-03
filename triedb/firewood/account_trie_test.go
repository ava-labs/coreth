// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"
)

type MyCoolType struct {
	Name string
	a, b uint
}

func TestXxx(t *testing.T) {
	var value interface{} = nil
	data, err := rlp.EncodeToBytes(value)
	require.NoError(t, err)
	fmt.Println(data)
	require.True(t, len(data) == 0)
}
