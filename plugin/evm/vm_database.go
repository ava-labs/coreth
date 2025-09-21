// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/database/meterblockdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/plugin/evm/database"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
)

const (
	blockDBFolder = "blockdb"
)

// initializeDBs initializes the databases used by the VM.
// coreth always uses the avalanchego provided database.
func (vm *VM) initializeDBs(db avalanchedatabase.Database) error {
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	vm.chaindb = rawdb.NewDatabase(database.WrapDatabase(prefixdb.NewNested(ethDBPrefix, db)))
	blockdbStateDB := prefixdb.New(blockDBPrefix, db)

	// do not allow reverting from block database to non-block database
	if database.IsBlockDBUsed(blockdbStateDB) && !vm.config.BlockDatabaseEnabled {
		log.Error("Once block database has been enabled, the VM cannot revert to not using it")
		return errors.New("cannot disable block database after it has been enabled")
	}

	if vm.config.BlockDatabaseEnabled {
		chaindb, err := vm.createBlockDatabaseChainDB(blockdbStateDB, vm.chaindb)
		if err != nil {
			return err
		}
		vm.chaindb = chaindb
	}
	vm.versiondb = versiondb.New(db)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.versiondb)
	vm.metadataDB = prefixdb.New(metadataPrefix, vm.versiondb)
	// Note warpDB is not part of versiondb because it is not necessary
	// that warp signatures are committed to the database atomically with
	// the last accepted block.
	vm.warpDB = prefixdb.New(warpPrefix, db)
	return nil
}

func (vm *VM) inspectDatabases() error {
	start := time.Now()
	log.Info("Starting database inspection")
	if err := rawdb.InspectDatabase(vm.chaindb, nil, nil); err != nil {
		return err
	}
	if err := inspectDB(vm.acceptedBlockDB, "acceptedBlockDB"); err != nil {
		return err
	}
	if err := inspectDB(vm.acceptedBlockDB, "acceptedBlockDB"); err != nil {
		return err
	}
	if err := inspectDB(vm.metadataDB, "metadataDB"); err != nil {
		return err
	}
	if err := inspectDB(vm.warpDB, "warpDB"); err != nil {
		return err
	}
	log.Info("Completed database inspection", "elapsed", time.Since(start))
	return nil
}

func inspectDB(db avalanchedatabase.Database, label string) error {
	it := db.NewIterator()
	defer it.Release()

	var (
		count  int64
		start  = time.Now()
		logged = time.Now()

		// Totals
		total common.StorageSize
	)
	// Inspect key-value database first.
	for it.Next() {
		var (
			key  = it.Key()
			size = common.StorageSize(len(key) + len(it.Value()))
		)
		total += size
		count++
		if count%1000 == 0 && time.Since(logged) > 8*time.Second {
			log.Info("Inspecting database", "label", label, "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	// Display the database statistic.
	log.Info("Database statistics", "label", label, "total", total.String(), "count", count)
	return nil
}

func (vm *VM) createBlockDatabaseChainDB(db avalanchedatabase.Database, ethDB ethdb.Database) (ethdb.Database, error) {
	versionPath := strconv.FormatUint(blockdb.IndexFileVersion, 10)
	blockDBPath := filepath.Join(vm.ctx.ChainDataDir, blockDBFolder, versionPath)
	config := blockdb.DefaultConfig().WithDir(blockDBPath).WithSyncToDisk(vm.config.BlockDatabaseSyncToDisk)
	blockDatabase, err := blockdb.New(config, vm.ctx.Log)
	if err != nil {
		return nil, err
	}
	meteredBlockDB, err := meterblockdb.New(vm.sdkMetrics, "blockdb", blockDatabase)
	if err != nil {
		return nil, err
	}
	chaindb, err := database.NewWrappedBlockDatabase(db, meteredBlockDB, ethDB, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create wrapped block database: %w", err)
	}
	return chaindb, nil
}
