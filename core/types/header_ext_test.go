// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderExtraGetWith(t *testing.T) {
	t.Parallel()

	h := &Header{}

	extra := GetHeaderExtra(h)
	require.NotNil(t, extra)
	assert.Equal(t, &HeaderExtra{}, extra)

	extra = &HeaderExtra{
		ExtDataHash: [32]byte{1},
	}
	WithHeaderExtra(h, extra)

	extra = GetHeaderExtra(h)
	assert.Equal(t, &HeaderExtra{
		ExtDataHash: [32]byte{1},
	}, extra)
}
