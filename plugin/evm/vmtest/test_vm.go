// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"context"
	"testing"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	commoneng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/stretchr/testify/require"
)

type TestVMConfig struct {
	IsSyncing bool
	Fork      *upgradetest.Fork
	// If genesisJSON is empty, defaults to the genesis corresponding to the
	// fork.
	GenesisJSON string
	ConfigJSON  string
}

type TestVMSuite struct {
	VM           commoneng.VM
	DB           *prefixdb.Database
	AtomicMemory *avalancheatomic.Memory
	AppSender    *enginetest.Sender
	Ctx          *snow.Context
}

// SetupTestVM initializes a VM for testing. It sets up the genesis and returns the
// issuer channel, database, atomic memory, app sender, and context.
// Expects the passed VM to be a uninitialized VM.
func SetupTestVM(t *testing.T, vm commoneng.VM, config TestVMConfig) *TestVMSuite {
	fork := upgradetest.Latest
	if config.Fork != nil {
		fork = *config.Fork
	}
	ctx, dbManager, genesisBytes, m := SetupGenesis(t, fork)
	if len(config.GenesisJSON) != 0 {
		genesisBytes = []byte(config.GenesisJSON)
	}
	appSender := &enginetest.Sender{
		T:                 t,
		CantSendAppGossip: true,
		SendAppGossipF:    func(context.Context, commonEng.SendConfig, []byte) error { return nil },
	}
	err := vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		nil,
		[]byte(config.ConfigJSON),
		nil,
		appSender,
	)
	require.NoError(t, err, "error initializing GenesisVM")

	if !config.IsSyncing {
		require.NoError(t, vm.SetState(context.Background(), snow.Bootstrapping))
		require.NoError(t, vm.SetState(context.Background(), snow.NormalOp))
	}

	return &TestVMSuite{
		VM:           vm,
		DB:           dbManager,
		AtomicMemory: m,
		AppSender:    appSender,
		Ctx:          ctx,
	}
}
