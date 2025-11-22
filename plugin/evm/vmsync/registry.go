// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/log"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/coreth/plugin/evm/message"

	synccommon "github.com/ava-labs/coreth/sync"
)

var (
	errSyncerAlreadyRegistered    = errors.New("syncer already registered")
	errUpdateSyncTargetNotAllowed = errors.New("UpdateSyncTarget not allowed while syncers are not running")
)

// SyncerTask represents a single syncer with its name for identification.
type SyncerTask struct {
	name   string
	syncer synccommon.Syncer
}

// SyncerRegistry manages a collection of syncers for sequential execution.
type SyncerRegistry struct {
	syncers         []SyncerTask
	registeredNames map[string]bool // Track registered IDs to prevent duplicates.

	// This lock protects access to target and updateTargetAllowed.
	targetLock          sync.Mutex
	target              message.Syncable
	updateTargetAllowed bool // Only allowed while syncers are running.
}

// NewSyncerRegistry creates a new empty syncer registry.
func NewSyncerRegistry() *SyncerRegistry {
	return &SyncerRegistry{
		registeredNames: make(map[string]bool),
	}
}

// Register adds a syncer to the registry.
// Returns an error if a syncer with the same name is already registered.
func (r *SyncerRegistry) Register(syncer synccommon.Syncer) error {
	id := syncer.ID()
	if r.registeredNames[id] {
		return fmt.Errorf("%w with id '%s'", errSyncerAlreadyRegistered, id)
	}

	r.registeredNames[id] = true
	r.syncers = append(r.syncers, SyncerTask{syncer.Name(), syncer})

	return nil
}

// RunSyncerTasks executes all registered syncers.
// The provided summary is used only for logging to decouple from concrete client types.
func (r *SyncerRegistry) RunSyncerTasks(ctx context.Context, summary message.Syncable) error {
	if len(r.syncers) == 0 {
		return nil
	}

	summaryBlockHashHex := summary.GetBlockHash().Hex()
	blockHeight := summary.Height()

	g, egCtx := errgroup.WithContext(ctx)

	for _, task := range r.syncers {
		g.Go(func() error {
			log.Info("starting syncer", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight)
			if err := task.syncer.Sync(egCtx); err != nil {
				log.Error("failed syncing", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight, "err", err)
				return fmt.Errorf("%s failed: %w", task.name, err)
			}
			log.Info("completed successfully", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight)

			return nil
		})
	}

	// Allow UpdateSyncTarget calls while waiting for syncers to complete.
	r.targetLock.Lock()
	r.updateTargetAllowed = true
	r.target = summary
	r.targetLock.Unlock()

	if err := g.Wait(); err != nil {
		return err
	}

	log.Info("all syncers completed successfully", "count", len(r.syncers), "summary", summaryBlockHashHex)

	// At this point, UpdateSyncTarget should no longer be called, so we should prevent it.
	r.targetLock.Lock()
	r.updateTargetAllowed = false
	finalTarget := r.target
	r.targetLock.Unlock()

	// Finalize each syncer to ensure all data matches the expected state.
	// Use a new error group to finalize in parallel.
	g, egCtx = errgroup.WithContext(ctx)
	for _, task := range r.syncers {
		log.Info("finalizing syncer", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight)
		g.Go(func() error {
			if err := task.syncer.Finalize(egCtx, finalTarget); err != nil {
				log.Error("failed finalizing", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight, "err", err)
				return fmt.Errorf("%s finalize failed: %w", task.name, err)
			}
			return nil
		})
	}

	return g.Wait()
}

// UpdateSyncTarget updates the target for all registered syncers.
func (r *SyncerRegistry) UpdateSyncTarget(summary message.Syncable) error {
	r.targetLock.Lock()
	defer r.targetLock.Unlock()

	if !r.updateTargetAllowed {
		return errUpdateSyncTargetNotAllowed
	}

	// Since UpdateSyncTarget can block, we call each syncer in its own goroutine.
	var eg errgroup.Group
	for _, task := range r.syncers {
		eg.Go(func() error {
			return task.syncer.UpdateSyncTarget(summary)
		})
	}
	if err := eg.Wait(); err != nil {
		return fmt.Errorf("failed to update sync target: %w", err)
	}

	r.target = summary
	return nil
}
