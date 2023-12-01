// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ethereum/go-ethereum/common"
)

func Script(
	chainID ids.ID, codec codec.Manager,
	main database.Database, bonusBlockHeights map[uint64]ids.ID,
	ssc syncclient.LeafClient, targetRoot common.Hash, targetHeight uint64,
) error {
	db := prefixdb.New([]byte("testing-stuff-001"), main)
	vdb := versiondb.New(db)
	atomic := atomic.NewMemory(db)
	sm := atomic.NewSharedMemory(chainID)
	repo, err := NewAtomicTxRepository(vdb, codec, 0, nil, nil, nil)
	if err != nil {
		return err
	}
	testBackend, err := NewAtomicBackend(
		vdb, sm, bonusBlockHeights, repo, 0, common.Hash{}, 4096)
	if err != nil {
		return err
	}
	syncer, err := testBackend.Syncer(ssc, targetRoot, targetHeight, defaultStateSyncRequestSize)
	if err != nil {
		return err
	}
	if err := syncer.Start(context.Background()); err != nil {
		return err
	}
	if err := <-syncer.Done(); err != nil {
		return err
	}
	if err := testBackend.MarkApplyToSharedMemoryCursor(0); err != nil {
		return err
	}
	if err := testBackend.ApplyToSharedMemory(targetHeight); err != nil {
		return err
	}
	return nil
}
