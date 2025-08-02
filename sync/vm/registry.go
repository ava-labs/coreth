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
	"golang.org/x/sync/errgroup"
)

var (
	errClientCannotProvideSummary = errors.New("client cannot provide a summary")
	errSyncerCannotBeNil          = errors.New("syncer cannot be nil")
	errCannotRegisterNewSyncer    = errors.New("cannot register new syncer due to sync already running")
	errCannotRunSyncerTasksTwice  = errors.New("cannot run syncer tasks again before previous run completes")
)

// SyncerTask represents a single syncer with its name for identification.
type SyncerTask struct {
	name   string
	syncer synccommon.Syncer
}

// SyncerRegistry manages a collection of syncers for concurrent execution.
type SyncerRegistry struct {
	syncers sync.Map    // key: string (name), value: SyncerTask
	started atomic.Bool // becomes true the first time RunSyncerTasks is called
}

// NewSyncerRegistry creates a new empty syncer registry.
func NewSyncerRegistry() *SyncerRegistry {
	return &SyncerRegistry{}
}

// Register adds a syncer to the registry.
// Returns an error if a syncer with the same name is already registered or if the syncer is nil.
func (r *SyncerRegistry) Register(name string, syncer synccommon.Syncer) error {
	// If the syncer registry has already been started, return an error.
	if r.started.Load() {
		return errCannotRegisterNewSyncer
	}

	// Use reflection to check for nil because in Go, a nil concrete type is not equal to a nil interface [synccommon.Syncer].
	// When a nil concrete type is assigned to an interface, the interface contains type information even though the value is nil.
	// reflect.ValueOf(syncer).IsNil() properly detects nil concrete types.
	if syncer == nil || reflect.ValueOf(syncer).IsNil() {
		return errSyncerCannotBeNil
	}

	task := SyncerTask{name, syncer}
	if _, loaded := r.syncers.LoadOrStore(name, task); loaded {
		return fmt.Errorf("syncer with name '%s' is already registered", name)
	}

	return nil
}

// RunSyncerTasks executes all registered syncers concurrently.
func (r *SyncerRegistry) RunSyncerTasks(ctx context.Context, client *client) error {
	// If the syncer registry has already been started, return an error.
	if !r.started.CompareAndSwap(false, true) {
		return errCannotRunSyncerTasksTwice
	}

	if client == nil || client.summary == nil {
		return errClientCannotProvideSummary
	}

	// Collect all syncers from the map.
	var syncers []SyncerTask
	r.syncers.Range(func(key, value any) bool {
		task := value.(SyncerTask)
		syncers = append(syncers, task)
		return true
	})

	summaryHex := client.summary.GetBlockHash().Hex()

	if len(syncers) == 0 {
		log.Info("no sync operations are configured to run", "summary", summaryHex)
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)

	// Use sync.Map to collect errors in a thread-safe way.
	var errorMap sync.Map

	for i, task := range syncers {
		log.Info("starting syncer", "name", task.name, "summary", summaryHex)

		g.Go(func() error {
			// Start syncer.
			if err := task.syncer.Start(ctx); err != nil {
				log.Error("failed to start", "name", task.name, "summary", summaryHex, "err", err)
				errorMap.Store(i, fmt.Errorf("failed to start %s: %w", task.name, err))
				return fmt.Errorf("failed to start %s: %w", task.name, err)
			}

			// Wait for completion.
			err := task.syncer.Wait(ctx)
			if err != nil {
				errorMap.Store(i, fmt.Errorf("%s failed: %w", task.name, err))
			}

			return err
		})
	}

	// Wait for all syncers to complete.
	if err := g.Wait(); err != nil {
		return fmt.Errorf("state sync failed: %w", err)
	}

	// Log completion results in registration order.
	for i, task := range syncers {
		if err, ok := errorMap.Load(i); ok {
			log.Error("failed to complete", "name", task.name, "summary", summaryHex, "err", err)
		} else {
			log.Info("completed successfully", "name", task.name, "summary", summaryHex)
		}
	}

	return nil
}
