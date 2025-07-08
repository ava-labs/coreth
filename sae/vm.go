// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"

	avalanchedb "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/coreth/core/state"
	corethdb "github.com/ava-labs/coreth/plugin/evm/database"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	sae "github.com/ava-labs/strevm"
)

type vm struct {
	*sae.VM // Populated by [vm.Initialize]
}

func (vm *vm) Initialize(
	ctx context.Context,
	chainContext *snow.Context,
	db avalanchedb.Database,
	genesisBytes []byte,
	configBytes []byte,
	upgradeBytes []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	ethDB := rawdb.NewDatabase(corethdb.WrapDatabase(db))

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return err
	}
	sdb := state.NewDatabase(ethDB)
	chainConfig, genesisHash, err := core.SetupGenesisBlock(ethDB, sdb.TrieDB(), genesis)
	if err != nil {
		return err
	}

	batch := ethDB.NewBatch()
	// Being both the "head" and "finalized" block is a requirement of [Config].
	rawdb.WriteHeadBlockHash(batch, genesisHash)
	rawdb.WriteFinalizedBlockHash(batch, genesisHash)
	if err := batch.Write(); err != nil {
		return err
	}

	vm.VM, err = sae.New(
		ctx,
		sae.Config{
			Hooks: &hooks{
				ctx:         chainContext,
				chainConfig: chainConfig,
				mempool:     nil, // TODO: populate me
			},
			ChainConfig: chainConfig,
			DB:          ethDB,
			LastSynchronousBlock: sae.LastSynchronousBlock{
				Hash:        genesisHash,
				Target:      acp176.MinTargetPerSecond,
				ExcessAfter: 0,
			},
			SnowCtx: chainContext,
		},
	)
	return err
}
