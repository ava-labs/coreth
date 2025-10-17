// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/x/blockdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/plugin/evm/database"
	evmblockdb "github.com/ava-labs/coreth/plugin/evm/database/blockdb"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
)

const (
	blockDBFolder = "blockdb"
)

// initializeDBs initializes the databases used by the VM.
// coreth always uses the avalanchego provided database.
func (vm *VM) initializeDBs(db avalanchedatabase.Database) error {
	vm.versiondb = versiondb.New(db)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.versiondb)
	vm.metadataDB = prefixdb.New(metadataPrefix, vm.versiondb)
	// Note warpDB is not part of versiondb because it is not necessary
	// that warp signatures are committed to the database atomically with
	// the last accepted block.
	vm.warpDB = prefixdb.New(warpPrefix, db)

	// chaindb must be created after acceptedBlockDB since it uses it
	chaindb, err := vm.newChainDB(db)
	if err != nil {
		return err
	}
	vm.chaindb = chaindb
	return nil
}

// newChainDB creates a new chain database
// If block database is enabled, it will wrap the chaindb with separate databases
// dedicated for storing blocks data.
// If block database is disable but was previously enabled, it will return an error.
func (vm *VM) newChainDB(db avalanchedatabase.Database) (ethdb.Database, error) {
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	chainDB := rawdb.NewDatabase(database.WrapDatabase(prefixdb.NewNested(ethDBPrefix, db)))

	// Error if block database has been enabled/created and then disabled
	stateDB := prefixdb.New(blockDBPrefix, db)
	enabled, err := evmblockdb.IsEnabled(stateDB)
	if err != nil {
		return nil, err
	}
	if !vm.config.BlockDatabaseEnabled {
		if enabled {
			return nil, errors.New("cannot disable block database after it has been enabled")
		}
		return chainDB, nil
	}

	versionPath := strconv.FormatUint(blockdb.IndexFileVersion, 10)
	blockDBPath := filepath.Join(vm.ctx.ChainDataDir, blockDBFolder, versionPath)
	hasLastAccepted, err := vm.acceptedBlockDB.Has(lastAcceptedKey)
	if err != nil {
		return nil, err
	}
	stateSyncEnabled := !hasLastAccepted
	if vm.config.StateSyncEnabled != nil {
		stateSyncEnabled = *vm.config.StateSyncEnabled
	}
	config := blockdb.DefaultConfig().WithSyncToDisk(vm.config.BlockDatabaseSyncToDisk)
	blockDB := evmblockdb.New(stateDB, chainDB, config, blockDBPath, vm.ctx.Log, vm.sdkMetrics)
	initialized, err := blockDB.InitWithStateSync(stateSyncEnabled)
	log.Info("blockDB initialized", "initialized", initialized, "stateSyncEnabled", stateSyncEnabled)
	if err != nil {
		return nil, err
	}
	if initialized && !vm.config.BlockDatabaseMigrationDisabled {
		if err := blockDB.Migrate(); err != nil {
			return nil, err
		}
	}
	return blockDB, nil
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
