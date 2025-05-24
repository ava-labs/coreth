// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestMarshalAtomicSyncSummary(t *testing.T) {
	atomicSyncSummary, err := NewAtomicSyncSummary(common.Hash{1}, 2, common.Hash{3}, common.Hash{4})
	require.NoError(t, err)

	require.Equal(t, common.Hash{1}, atomicSyncSummary.GetBlockHash())
	require.Equal(t, uint64(2), atomicSyncSummary.Height())
	require.Equal(t, common.Hash{3}, atomicSyncSummary.GetBlockRoot())

	expectedBase64Bytes := "AAAAAAAAAAAAAgEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	require.Equal(t, expectedBase64Bytes, base64.StdEncoding.EncodeToString(atomicSyncSummary.Bytes()))

	parser := NewAtomicSyncSummaryParser()
	called := false
	acceptImplTest := func(message.Syncable) (block.StateSyncMode, error) {
		called = true
		return block.StateSyncSkipped, nil
	}
	s, err := parser.ParseFromBytes(atomicSyncSummary.Bytes(), acceptImplTest)
	require.NoError(t, err)
	require.Equal(t, atomicSyncSummary.GetBlockHash(), s.GetBlockHash())
	require.Equal(t, atomicSyncSummary.Height(), s.Height())
	require.Equal(t, atomicSyncSummary.GetBlockRoot(), s.GetBlockRoot())
	require.Equal(t, atomicSyncSummary.AtomicRoot, s.(*AtomicSyncSummary).AtomicRoot)
	require.Equal(t, atomicSyncSummary.Bytes(), s.Bytes())

	mode, err := s.Accept(context.TODO())
	require.NoError(t, err)
	require.Equal(t, block.StateSyncSkipped, mode)
	require.True(t, called)
}
