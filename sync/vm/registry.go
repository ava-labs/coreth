// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"fmt"
	"sync"

	synccommon "github.com/ava-labs/coreth/sync"
	"github.com/ava-labs/libevm/log"
	"golang.org/x/sync/errgroup"
)

// SyncerTask represents a single syncer with its name for identification.
type SyncerTask struct {
	name   string
	syncer synccommon.Syncer
}

// SyncerRegistry manages a collection of syncers for concurrent execution.
type SyncerRegistry struct {
	syncers sync.Map // key: string (name), value: SyncerTask
}

// NewSyncerRegistry creates a new empty syncer registry.
func NewSyncerRegistry() *SyncerRegistry {
	return &SyncerRegistry{}
}

// Register adds a syncer to the registry.
// Returns an error if a syncer with the same name is already registered.
func (r *SyncerRegistry) Register(name string, syncer synccommon.Syncer) error {
	task := SyncerTask{name, syncer}
	if _, loaded := r.syncers.LoadOrStore(name, task); loaded {
		return fmt.Errorf("syncer with name '%s' is already registered", name)
	}

	return nil
}

// RunSyncerTasks executes all registered syncers concurrently.
func (r *SyncerRegistry) RunSyncerTasks(ctx context.Context, client *client) error {
	// Collect all syncers from the map
	var syncers []SyncerTask
	r.syncers.Range(func(key, value any) bool {
		task := value.(SyncerTask)
		syncers = append(syncers, task)
		return true
	})

	if len(syncers) == 0 {
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)

	// Use sync.Map to collect errors in a thread-safe way.
	var errorMap sync.Map

	// Calculate summary string once since client doesn't change
	summaryStr := ""
	if client != nil && client.summary != nil {
		summaryStr = client.summary.GetBlockHash().String()
	}

	for i, task := range syncers {
		log.Info("starting syncer", "name", task.name, "summary", summaryStr)

		g.Go(func() error {
			// Start syncer.
			if err := task.syncer.Start(ctx); err != nil {
				log.Error("failed to start", "name", task.name, "summary", summaryStr, "err", err)
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
			log.Error("failed to complete", "name", task.name, "summary", summaryStr, "err", err)
		} else {
			log.Info("completed successfully", "name", task.name, "summary", summaryStr)
		}
	}

	return nil
}
