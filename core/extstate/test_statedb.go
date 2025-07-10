// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package extstate

import (
	"testing"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

type TestStateDB struct {
	*StateDB
}

func NewTest(t testing.TB) *TestStateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &TestStateDB{
		StateDB: New(statedb),
	}
}

// SetPredicateStorageSlots allows overwriting predicate storage slots for the
// given address during tests.
func (s *TestStateDB) SetPredicateStorageSlots(address common.Address, predicates [][]byte) {
	s.predicateStorageSlots[address] = predicates
}
