// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/stretchr/testify/require"
)

func TestScript(t *testing.T) {
	vm := &VM{}
	vm.ctx = NewContext()
	vm.codec = message.Codec
	targetHeight := uint64(4096 * 10)
	atomicTrie := mkSyncerTrie(t, targetHeight)
	mockClient := syncclient.NewMockClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(atomicTrie.trieDB, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	db := memdb.New()
	err := Script(vm.ctx.ChainID, vm.codec, db, nil, mockClient, atomicTrie.lastAcceptedRoot, targetHeight)
	require.NoError(t, err)
}
