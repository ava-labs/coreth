// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestXxx(t *testing.T) {
	require := require.New(t)
	db := rawdb.NewMemoryDatabase()
	database := state.NewDatabase(db)

	targetAddr := common.HexToAddress("0xc0c5aa69dbe4d6dddfbc89c0957686ec60f24389")
	numKeysToUpdate := 5
	stateRoot := common.Hash{}

	for i := 0; i < 10; i++ {
		var err error
		stateRoot, err = addKeys(database, stateRoot, i, numKeysToUpdate, targetAddr)
		require.NoError(err)
		t.Logf("stateRoot: %x", stateRoot)
	}
}
