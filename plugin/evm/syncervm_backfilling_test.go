// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestPackUnpackHeights(t *testing.T) {
	require := require.New(t)
	tests := [][]heightInterval{
		[]heightInterval{
			heightInterval{
				upperBound: 10,
				lowerBound: 2,
			},
			heightInterval{
				upperBound: 100,
				lowerBound: 30,
			},
		},
		[]heightInterval{},
	}

	for _, test := range tests {
		heights := test
		b, err := packHeightIntervals(heights)
		require.NoError(err)

		res, err := unpackHeightIntervals(b)
		require.NoError(err)
		require.Equal(heights, res)
	}
}

func TestBlockDownloadStart(t *testing.T) {
	require := require.New(t)

	var (
		baseDB     = memdb.New()
		db         = versiondb.New(baseDB)
		metadataDB = versiondb.New(baseDB)

		blocks = make([]*Block, 0)
	)

	dt := DownloadsTracker{
		metadataDB: metadataDB,
		db:         db,
		getBlk: func(_ context.Context, blkID ids.ID) (*Block, error) {
			for _, blk := range blocks {
				if blk.ID() == blkID {
					return blk, nil
				}
			}
			return nil, database.ErrNotFound
		},
		getBlkIDAtHeigth: func(_ context.Context, h uint64) (ids.ID, error) {
			for _, blk := range blocks {
				if blk.Height() == h {
					return blk.ID(), nil
				}
			}
			return ids.Empty, database.ErrNotFound
		},
	}

	var (
		firstStateSummaryHeight  = big.NewInt(314)
		secondStateSummaryHeight = new(big.Int).Mul(firstStateSummaryHeight, big.NewInt(2))
	)

	{
		// no state summary nor ever backfilled. Backfill won't run
		_, _, err := dt.StartHeight(context.Background(), ids.Empty)
		require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)
	}

	{
		// state summary available, block backfilling starts from there
		firstStateSummaryBlk := &Block{
			id: ids.GenerateTestID(),
			ethBlock: types.NewBlock(&types.Header{
				Number:     firstStateSummaryHeight,
				ParentHash: common.Hash(ids.GenerateTestID()),
			},
				nil, nil, nil, nil, nil, true),
		}
		blocks = append(blocks, firstStateSummaryBlk)

		nextBlkID, nextBlkHeight, err := dt.StartHeight(context.Background(), firstStateSummaryBlk.ID())
		require.NoError(err)
		require.Equal(firstStateSummaryBlk.Parent(), nextBlkID)
		require.Equal(firstStateSummaryBlk.Height()-1, nextBlkHeight)
	}

	{
		// another state summary available, while first block backfilling is not done.
		// We restart from second state summary, which is the highest
		secondStateSummaryBlk := &Block{
			id: ids.GenerateTestID(),
			ethBlock: types.NewBlock(&types.Header{
				Number:     secondStateSummaryHeight,
				ParentHash: common.Hash(ids.GenerateTestID()),
			}, nil, nil, nil, nil, nil, true),
		}
		blocks = make([]*Block, 0) // wipe previous state summary
		blocks = append(blocks, secondStateSummaryBlk)

		nextBlkID, nextBlkHeight, err := dt.StartHeight(context.Background(), secondStateSummaryBlk.ID())
		require.NoError(err)
		require.Equal(secondStateSummaryBlk.Parent(), nextBlkID)
		require.Equal(secondStateSummaryBlk.Height()-1, nextBlkHeight)
	}
}

func TestBlockDownloadNext(t *testing.T) {
	require := require.New(t)

	var (
		baseDB     = memdb.New()
		db         = versiondb.New(baseDB)
		metadataDB = versiondb.New(baseDB)

		blocks = make([]*Block, 0)
	)

	dt := DownloadsTracker{
		metadataDB: metadataDB,
		db:         db,
		getBlk: func(_ context.Context, blkID ids.ID) (*Block, error) {
			for _, blk := range blocks {
				if blk.ID() == blkID {
					return blk, nil
				}
			}
			return nil, database.ErrNotFound
		},
		getBlkIDAtHeigth: func(_ context.Context, h uint64) (ids.ID, error) {
			for _, blk := range blocks {
				if blk.Height() == h {
					return blk.ID(), nil
				}
			}
			return ids.Empty, database.ErrNotFound
		},
	}

	// start from state summary
	firstStateSummaryHeight := big.NewInt(314)
	firstStateSummaryBlk := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     firstStateSummaryHeight,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, firstStateSummaryBlk)

	nextBlkID, nextBlkHeight, err := dt.StartHeight(context.Background(), firstStateSummaryBlk.ID())
	require.NoError(err)
	require.Equal(firstStateSummaryBlk.Parent(), nextBlkID)
	require.Equal(firstStateSummaryBlk.Height()-1, nextBlkHeight)

	// accept a bunch of blocks
	var (
		backfilledHeight1 = new(big.Int).Div(firstStateSummaryHeight, big.NewInt(2))
		backfilledHeight2 = new(big.Int).Div(backfilledHeight1, big.NewInt(2))
		backfilledHeight3 = big.NewInt(1)
	)

	// first backfilled block
	backfilledBlk1 := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     backfilledHeight1,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, backfilledBlk1)

	nextBlkID, nextBlkHeight, err = dt.NextHeight(context.Background(), backfilledBlk1)
	require.NoError(err)
	require.Equal(backfilledBlk1.Parent(), nextBlkID)
	require.Equal(backfilledBlk1.Height()-1, nextBlkHeight)

	// second backfilled block
	backfilledBlk2 := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     backfilledHeight2,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, backfilledBlk1)

	nextBlkID, nextBlkHeight, err = dt.NextHeight(context.Background(), backfilledBlk2)
	require.NoError(err)
	require.Equal(backfilledBlk2.Parent(), nextBlkID)
	require.Equal(backfilledBlk2.Height()-1, nextBlkHeight)

	// final backfilled block. Height 1 is the last block that can be backfilled.
	backfilledBlk3 := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     backfilledHeight3,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, backfilledBlk1)

	_, _, err = dt.NextHeight(context.Background(), backfilledBlk3)
	require.ErrorIs(err, block.ErrStopBlockBackfilling)

	// Once last block is backfilled, backfilling won't restart
	_, _, err = dt.StartHeight(context.Background(), ids.Empty)
	require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)
}

func TestBlockDownloadNextWithMultipleStateSyncRuns(t *testing.T) {
	require := require.New(t)

	var (
		baseDB     = memdb.New()
		db         = versiondb.New(baseDB)
		metadataDB = versiondb.New(baseDB)

		blocks = make([]*Block, 0)
	)

	dt := DownloadsTracker{
		metadataDB: metadataDB,
		db:         db,
		getBlk: func(_ context.Context, blkID ids.ID) (*Block, error) {
			for _, blk := range blocks {
				if blk.ID() == blkID {
					return blk, nil
				}
			}
			return nil, database.ErrNotFound
		},
		getBlkIDAtHeigth: func(_ context.Context, h uint64) (ids.ID, error) {
			for _, blk := range blocks {
				if blk.Height() == h {
					return blk.ID(), nil
				}
			}
			return ids.Empty, database.ErrNotFound
		},
	}

	var (
		firstStateSummaryHeight   = big.NewInt(314)
		firstRunLatestBlockHeight = new(big.Int).Div(firstStateSummaryHeight, big.NewInt(2))
		secondStateSummaryHeight  = new(big.Int).Mul(firstStateSummaryHeight, big.NewInt(2))
	)

	// First run: we start from first state summary and backfill some blocks, till backfilledBlk1
	firstStateSummaryBlk := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     firstStateSummaryHeight,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, firstStateSummaryBlk)

	nextBlkID, nextBlkHeight, err := dt.StartHeight(context.Background(), firstStateSummaryBlk.ID())
	require.NoError(err)
	require.Equal(firstStateSummaryBlk.Parent(), nextBlkID)
	require.Equal(firstStateSummaryBlk.Height()-1, nextBlkHeight)

	backfilledBlk1 := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     firstRunLatestBlockHeight,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, backfilledBlk1)

	nextBlkID, nextBlkHeight, err = dt.NextHeight(context.Background(), backfilledBlk1)
	require.NoError(err)
	require.Equal(backfilledBlk1.Parent(), nextBlkID)
	require.Equal(backfilledBlk1.Height()-1, nextBlkHeight)

	// Second run: we start from a higher state summary and backfill the full gap till first state summary height
	secondStateSummaryBlk := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     secondStateSummaryHeight,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, secondStateSummaryBlk)

	nextBlkID, nextBlkHeight, err = dt.StartHeight(context.Background(), secondStateSummaryBlk.ID())
	require.NoError(err)
	require.Equal(secondStateSummaryBlk.Parent(), nextBlkID) // we start filling highest gaps
	require.Equal(secondStateSummaryBlk.Height()-1, nextBlkHeight)

	secondBlkHeight := new(big.Int).Add(firstStateSummaryHeight, big.NewInt(1)) // assume we just filled the highest block gap
	backfilledBlk2 := &Block{
		id: ids.GenerateTestID(),
		ethBlock: types.NewBlock(&types.Header{
			Number:     secondBlkHeight,
			ParentHash: common.Hash(ids.GenerateTestID()),
		}, nil, nil, nil, nil, nil, true),
	}
	blocks = append(blocks, backfilledBlk2)

	// We expect that the system jumpt to request lowest gap, without requesting again blocks
	// already downloaded in the first backfilling run
	nextBlkID, nextBlkHeight, err = dt.NextHeight(context.Background(), backfilledBlk2)
	require.NoError(err)
	require.Equal(backfilledBlk1.Parent(), nextBlkID)
	require.Equal(backfilledBlk1.Height()-1, nextBlkHeight)
}
