// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncpkg "github.com/ava-labs/coreth/sync"
	"github.com/ava-labs/libevm/log"
)

var _ extension.Client = (*dynamicClient)(nil)

type dynamicClient struct {
	*client
	syncTracker *SyncTracker
}

func NewDynamicClient(config *ClientConfig) extension.Client {
	client := NewClient(config).(*client)
	return &dynamicClient{
		client,
		&SyncTracker{},
	}
}

func (client *dynamicClient) AddTransition(ctx context.Context, block extension.ExtendedBlock, st extension.StateTransition) (bool, error) {
	return client.syncTracker.Run(ctx, block, st)
}

// GetOngoingSyncStateSummary returns a state summary that was previously started
// and not finished, and sets [resumableSummary] if one was found.
// Returns [database.ErrNotFound] if no ongoing summary is found or if [client.skipResume] is true.
func (client *dynamicClient) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	if client.SkipResume {
		return nil, database.ErrNotFound
	}

	summaryBytes, err := client.MetadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := client.Parser.Parse(summaryBytes, client.acceptSyncSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	client.resumableSummary = summary
	return summary, nil
}

// ClearOngoingSummary clears any marker of an ongoing state sync summary
func (client *dynamicClient) ClearOngoingSummary() error {
	if err := client.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return fmt.Errorf("failed to clear ongoing summary: %w", err)
	}
	if err := client.VerDB.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	return nil
}

// ParseStateSummary parses [summaryBytes] to [commonEng.Summary]
func (client *dynamicClient) ParseStateSummary(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return client.Parser.Parse(summaryBytes, client.acceptSyncSummary)
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (client *dynamicClient) acceptSyncSummary(proposedSummary message.Syncable) (block.StateSyncMode, error) {
	isResume := client.resumableSummary != nil &&
		proposedSummary.GetBlockHash() == client.resumableSummary.GetBlockHash()
	if !isResume {
		// Skip syncing if the blockchain is not significantly ahead of local state,
		// since bootstrapping would be faster.
		// (Also ensures we don't sync to a height prior to local state.)
		if client.LastAcceptedHeight+client.MinBlocks > proposedSummary.Height() {
			log.Info(
				"last accepted too close to most recent syncable block, skipping state sync",
				"lastAccepted", client.LastAcceptedHeight,
				"syncableHeight", proposedSummary.Height(),
			)
			return block.StateSyncSkipped, nil
		}
	}
	client.summary = proposedSummary

	// Update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := client.MetadataDB.Put(stateSyncSummaryKey, proposedSummary.Bytes()); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := client.VerDB.Commit(); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to commit db: %w", err)
	}

	log.Info("Starting state sync", "summary", proposedSummary)

	// create a cancellable ctx for the state sync goroutine
	ctx, cancel := context.WithCancel(context.Background())
	client.cancel = cancel
	client.wg.Add(1) // track the state sync goroutine so we can wait for it on shutdown
	go func() {
		defer client.wg.Done()
		defer cancel()

		if err := client.stateSync(ctx); err != nil {
			client.err = err
		} else {
			client.err = client.finishSync()
		}
		// notify engine regardless of whether err == nil,
		// this error will be propagated to the engine when it calls
		// vm.SetState(snow.Bootstrapping)
		log.Info("stateSync completed, notifying engine", "err", client.err)
		close(client.StateSyncDone)
	}()
	return block.StateSyncDynamic, nil
}
func (client *dynamicClient) stateSync(ctx context.Context) error {
	// Create and register all syncers.
	if err := client.registerSyncers(); err != nil {
		return fmt.Errorf("failed to register syncers: %w", err)
	}
	client.syncTracker.Sync(ctx)

	return nil
}

func (client *dynamicClient) registerSyncers() error {
	// Register block syncer.
	blockSyncer, err := client.createBlockSyncer(client.summary.GetBlockHash(), client.summary.Height())
	if err != nil {
		return fmt.Errorf("failed to create block syncer: %w", err)
	}

	codeQueue, err := client.createCodeQueue()
	if err != nil {
		return fmt.Errorf("failed to create code queue: %w", err)
	}

	codeSyncer, err := client.createCodeSyncer(codeQueue.CodeHashes())
	if err != nil {
		return fmt.Errorf("failed to create code syncer: %w", err)
	}

	// TODO: add dynamic state syncer
	stateSyncer, err := client.createEVMSyncer(codeQueue)
	if err != nil {
		return fmt.Errorf("failed to create EVM state syncer: %w", err)
	}

	var atomicSyncer syncpkg.Syncer
	if client.Extender != nil {
		atomicSyncer, err = client.createAtomicSyncer()
		if err != nil {
			return fmt.Errorf("failed to create atomic syncer: %w", err)
		}
	}

	syncers := []syncpkg.Syncer{
		blockSyncer,
		codeSyncer,
		stateSyncer,
	}
	if atomicSyncer != nil {
		syncers = append(syncers, atomicSyncer)
	}

	for _, s := range syncers {
		if err := client.syncTracker.RegisterSyncer(s); err != nil {
			return fmt.Errorf("failed to register %s syncer: %w", s.Name(), err)
		}
	}

	return nil
}
