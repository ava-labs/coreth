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

	// Phase 1: Start all syncers concurrently.
	startGroup, _ := errgroup.WithContext(ctx)
	for _, task := range syncers {
		startGroup.Go(func() error {
			log.Info("starting syncer", "name", task.name, "summary", summaryHex)

			if err := task.syncer.Start(ctx); err != nil {
				log.Error("syncer failed to start", "name", task.name, "summary", summaryHex, "err", err)
				return fmt.Errorf("failed to start %s: %w", task.name, err)
			}

			log.Info("syncer started successfully", "name", task.name, "summary", summaryHex)
			return nil
		})
	}

	// Wait for all syncers to start.
	if err := startGroup.Wait(); err != nil {
		return fmt.Errorf("sync start failed: %w", err)
	}

	log.Info("all syncers started", "count", len(syncers))

	// Phase 2: Wait for all syncers to complete.
	waitGroup, _ := errgroup.WithContext(ctx)
	for _, task := range syncers {
		waitGroup.Go(func() error {
			res := task.syncer.Wait(ctx)

			// If it was aborted via its own cancellation, treat as success.
			if res.Cancelled || errors.Is(res.Err, context.Canceled) {
				log.Info("syncer aborted cleanly", "name", task.name, "summary", summaryHex)
				return nil
			}

			// Any real error bubbles up to the errgroup.
			if res.Err != nil {
				log.Error("syncer failed to complete", "name", task.name, "summary", summaryHex, "err", res.Err)
				return fmt.Errorf("failed to complete %s: %w", task.name, res.Err)
			}

			// On clean completion, we don't return an error.
			log.Info("syncer completed successfully", "name", task.name, "summary", summaryHex)
			return nil
		})
	}

	// Wait for all syncers to complete.
	if err := waitGroup.Wait(); err != nil {
		return fmt.Errorf("sync execution failed: %w", err)
	}

	// If our parent ctx was cancelled, return that cancelation now.
	// This is mostly to handle client.Shutdown() or hitting a timeout.
	if err := ctx.Err(); err != nil {
		return err
	}

	log.Info("all syncers completed successfully", "summary", summaryHex)

	return nil
}
