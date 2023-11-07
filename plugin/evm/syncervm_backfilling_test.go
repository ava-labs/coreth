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
			id:       ids.GenerateTestID(),
			ethBlock: types.NewBlock(&types.Header{Number: firstStateSummaryHeight}, nil, nil, nil, nil, nil, true),
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
			id:       ids.GenerateTestID(),
			ethBlock: types.NewBlock(&types.Header{Number: secondStateSummaryHeight}, nil, nil, nil, nil, nil, true),
		}
		blocks = make([]*Block, 0) // wipe previous state summary
		blocks = append(blocks, secondStateSummaryBlk)

		nextBlkID, nextBlkHeight, err := dt.StartHeight(context.Background(), secondStateSummaryBlk.ID())
		require.NoError(err)
		require.Equal(secondStateSummaryBlk.Parent(), nextBlkID)
		require.Equal(secondStateSummaryBlk.Height()-1, nextBlkHeight)
	}
}
