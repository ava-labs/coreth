// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	synccommon "github.com/ava-labs/coreth/sync"
	"github.com/ava-labs/libevm/log"
)

var (
	errClientCannotProvideSummary = errors.New("client must provide a summary")
	errSyncerCannotBeNil          = errors.New("syncer cannot be nil")
	errCannotRegisterNewSyncer    = errors.New("cannot register new syncer due to sync already running")
	errCannotRunSyncerTasksTwice  = errors.New("cannot run syncer tasks again before previous run completes")
	errEmptySyncerName            = errors.New("syncer name cannot be empty")
)

// SyncerTask represents a single syncer with its name for identification.
type SyncerTask struct {
	name   string
	syncer synccommon.Syncer
}

// SyncerRegistry manages a collection of syncers for sequential execution.
type SyncerRegistry struct {
	// key: string (name), value: SyncerTask.
	// Serves the purpose of storing the syncers in a map, ded
	syncers sync.Map
	// becomes true the first time RunSyncerTasks is called.
	started atomic.Bool
	// Stores []SyncerTask after first start for lock-free iteration.
	// NOTE: This will be needed when we introduce parallelization and UpdateSyncTarget in the [synccommon.Syncer] interface,
	// because we will need to iterate over the syncers in a lock-free manner.
	frozen atomic.Value
}

// NewSyncerRegistry creates a new empty syncer registry.
func NewSyncerRegistry() *SyncerRegistry {
	r := &SyncerRegistry{}
	// Initialize frozen with an empty slice so loads are always safe.
	r.frozen.Store([]SyncerTask(nil))
	return r
}

// Register adds a syncer to the registry.
// Returns an error if a syncer with the same name is already registered.
func (r *SyncerRegistry) Register(name string, syncer synccommon.Syncer) error {
	// If the syncer registry has already been started, return an error.
	if r.started.Load() {
		return errCannotRegisterNewSyncer
	}

	// Validate that the name is not empty.
	if name == "" {
		return errEmptySyncerName
	}

	// Use reflection to check for nil because in Go, a nil concrete type is not equal to a nil interface [synccommon.Syncer].
	// When a nil concrete type is assigned to an interface, the interface contains type information even though the value is nil.
	// reflect.ValueOf(syncer).IsNil() properly detects nil concrete types.
	if syncer == nil || reflect.ValueOf(syncer).IsNil() {
		return errSyncerCannotBeNil
	}

	task := SyncerTask{name, syncer}
	if _, loaded := r.syncers.LoadOrStore(name, task); loaded {
		return fmt.Errorf("syncer is already registered: %s", name)
	}

	return nil
}

// RunSyncerTasks executes all registered syncers.
func (r *SyncerRegistry) RunSyncerTasks(ctx context.Context, client *client) error {
	// If the syncer registry has already been started, return an error.
	if !r.started.CompareAndSwap(false, true) {
		return errCannotRunSyncerTasksTwice
	}

	if client == nil || client.summary == nil {
		return errClientCannotProvideSummary
	}

	// Collect all syncers from the map and freeze them for subsequent lock-free iteration.
	var syncers []SyncerTask
	r.syncers.Range(func(key, value any) bool {
		task := value.(SyncerTask)
		syncers = append(syncers, task)
		return true
	})
	r.frozen.Store(syncers)

	summaryHex := client.summary.GetBlockHash().Hex()

	if len(syncers) == 0 {
		log.Info("no sync operations are configured to run", "summary", summaryHex)
		return nil
	}

	// Iterate the frozen snapshot.
	for _, task := range r.frozen.Load().([]SyncerTask) {
		log.Info("starting syncer", "name", task.name, "summary", client.summary)

		// Start syncer.
		if err := task.syncer.Start(ctx); err != nil {
			log.Info("failed to start", "name", task.name, "summary", client.summary, "err", err)
			return fmt.Errorf("failed to start %s: %w", task.name, err)
		}

		// Wait for completion.
		err := task.syncer.Wait(ctx)

		// Log completion immediately.
		if err != nil {
			log.Error("failed to complete", "name", task.name, "summary", client.summary, "err", err)
			return fmt.Errorf("%s failed: %w", task.name, err)
		} else {
			log.Info("completed successfully", "name", task.name, "summary", client.summary)
		}
	}

	log.Info("all syncers completed successfully", "summary", summaryHex)

	return nil
}
