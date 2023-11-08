// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ethereum/go-ethereum/log"
)

var downloadedHeightsKey = []byte("downloadedHeights")

type heightInterval struct {
	upperBound uint64
	lowerBound uint64
}

func packHeightIntervals(heights []heightInterval) ([]byte, error) {
	size := wrappers.IntLen + wrappers.LongLen*2*len(heights)
	p := wrappers.Packer{Bytes: make([]byte, size)}
	p.PackInt(uint32(len(heights)))
	for _, h := range heights {
		p.PackLong(h.upperBound)
		p.PackLong(h.lowerBound)
	}
	return p.Bytes, p.Err
}

func unpackHeightIntervals(b []byte) ([]heightInterval, error) {
	p := wrappers.Packer{Bytes: b}
	heightsLen := p.UnpackInt()
	if p.Errored() {
		return nil, p.Err
	}
	res := make([]heightInterval, 0, heightsLen)
	for i := 0; i < int(heightsLen); i++ {
		up := p.UnpackLong()
		if p.Errored() {
			return nil, p.Err
		}
		lo := p.UnpackLong()
		if p.Errored() {
			return nil, p.Err
		}
		res = append(res, heightInterval{
			upperBound: up,
			lowerBound: lo,
		})
	}
	return res, nil
}

type DownloadsTracker struct {
	metadataDB database.Database
	db         *versiondb.Database

	getBlk           func(context.Context, ids.ID) (*Block, error)
	getBlkIDAtHeigth func(context.Context, uint64) (ids.ID, error)

	// block backfilling is a lengthy process, so multiple state sync may complete
	// before block backfilling does. We track downloaded blocks to avoid gaps in
	// block indexes as well as multiple downloads of the same blocks
	backfilledHeights []heightInterval
}

// Note: engine guarantee to call BackfillBlocksEnabled only after StateSyncDone event has been issued
func (dt *DownloadsTracker) StartHeight(ctx context.Context, stateSummaryBlk ids.ID) (ids.ID, uint64, error) {
	var (
		nextBlkID     ids.ID
		nextBlkHeight uint64
	)

	// pull latest summary first. If available, it may extend the range
	// of block heights to be downloaded
	summaryBlk, err := dt.getBlk(ctx, stateSummaryBlk)
	switch err {
	case nil:
		// nothing to do here
	case database.ErrNotFound:
		summaryBlk = nil // make sure this is nit
	default:
		return ids.Empty, 0, fmt.Errorf(
			"failed retrieving summary block %s: %w, %w",
			stateSummaryBlk,
			err,
			block.ErrInternalBlockBackfilling,
		)
	}

	// load block heights that where under processing. Possibly extend them with latest state summary
	switch dhBytes, err := dt.metadataDB.Get(downloadedHeightsKey); err {
	case database.ErrNotFound: // backfill was not ongoing
		if summaryBlk == nil {
			log.Info("Can't find state summary nor block to start backfilling from. Skipping backfilling")
			return ids.Empty, 0, block.ErrBlockBackfillingNotEnabled
		}

		// Start backfilling from current state summary
		nextBlkID = summaryBlk.Parent()
		nextBlkHeight = summaryBlk.Height() - 1

		dt.backfilledHeights = []heightInterval{
			heightInterval{
				upperBound: summaryBlk.Height(),
				lowerBound: summaryBlk.Height(),
			},
		}

		if err := dt.storeBlockHeights(dt.backfilledHeights); err != nil {
			return ids.Empty, 0, err
		}

	case nil:
		// backfill was ongoing. Resume from latest backfilled block
		dh, err := unpackHeightIntervals(dhBytes)
		if err != nil {
			return ids.Empty, 0, fmt.Errorf(
				"failed parsing downloaded height: %w, %w",
				err,
				block.ErrInternalBlockBackfilling,
			)
		}

		if summaryBlk != nil {
			// extend height range to backfill from and store it
			if len(dh) == 0 || (len(dh) > 0 && summaryBlk.Height() != dh[0].upperBound) {
				dh = append([]heightInterval{
					{
						upperBound: summaryBlk.Height(),
						lowerBound: summaryBlk.Height(),
					},
				}, dh...)

				if err := dt.storeBlockHeights(dh); err != nil {
					return ids.Empty, 0, err
				}
			}
		}

		dt.backfilledHeights = dh
		latestBackfilledBlk, err := dt.getBlockAtHeight(ctx, dh[0].lowerBound)
		if err != nil {
			return ids.Empty, 0, err
		}
		nextBlkID = latestBackfilledBlk.Parent()
		nextBlkHeight = latestBackfilledBlk.Height() - 1

	default:
		return ids.Empty, 0, fmt.Errorf(
			"failed retrieving last backfilled block ID from disk: %w, %w",
			err,
			block.ErrInternalBlockBackfilling,
		)
	}

	return nextBlkID, nextBlkHeight, nil
}

func (dt *DownloadsTracker) NextHeight(ctx context.Context, latestBlk *Block) (ids.ID, uint64, error) {
	if latestBlk.Height() == 1 { // done backfilling
		dt.backfilledHeights = []heightInterval{}
		if err := dt.metadataDB.Delete(downloadedHeightsKey); err != nil {
			return ids.Empty, 0, fmt.Errorf(
				"failed clearing downloaded heights from disk: %w, %w",
				err,
				block.ErrInternalBlockBackfilling,
			)
		}
		if err := dt.db.Commit(); err != nil {
			return ids.Empty, 0, fmt.Errorf("failed to commit db: %w, %w",
				err,
				block.ErrInternalBlockBackfilling,
			)
		}

		log.Info("block backfilling completed")
		return ids.Empty, 0, block.ErrStopBlockBackfilling
	}

	var (
		nextBlkID     = latestBlk.Parent()
		nextBlkHeight = latestBlk.Height() - 1
	)

	// update latest backfilled block and possibly merge contiguous height intervals
	dt.backfilledHeights[0].lowerBound = latestBlk.Height()
	for len(dt.backfilledHeights) > 1 && dt.backfilledHeights[0].lowerBound <= dt.backfilledHeights[1].upperBound+1 {
		nextLowerBound := safemath.Min(dt.backfilledHeights[0].lowerBound, dt.backfilledHeights[1].lowerBound)
		dt.backfilledHeights[0].lowerBound = nextLowerBound

		// drop height interval at position 1 (merged)
		if len(dt.backfilledHeights) > 2 {
			dt.backfilledHeights = append(dt.backfilledHeights[:1], dt.backfilledHeights[2:]...)
		} else {
			dt.backfilledHeights = dt.backfilledHeights[:len(dt.backfilledHeights)-1]
		}

		nextBlkHeight = nextLowerBound - 1
	}

	if nextBlkHeight != latestBlk.Height()-1 {
		// merge of backfilled heights happened. nextBlkHeight is set right,
		// nextBlkID must be updated
		latestBackfilledBlk, err := dt.getBlockAtHeight(ctx, nextBlkHeight+1)
		if err != nil {
			return ids.Empty, 0, err
		}
		nextBlkID = latestBackfilledBlk.Parent()
	}
	if err := dt.storeBlockHeights(dt.backfilledHeights); err != nil {
		return ids.Empty, 0, err
	}

	return nextBlkID, nextBlkHeight, nil
}

func (dt *DownloadsTracker) storeBlockHeights(h []heightInterval) error {
	hi, err := packHeightIntervals(dt.backfilledHeights)
	if err != nil {
		return fmt.Errorf(
			"failed packing heights interval: %w, %w",
			err,
			block.ErrInternalBlockBackfilling,
		)
	}
	if err := dt.metadataDB.Put(downloadedHeightsKey, hi); err != nil {
		return fmt.Errorf(
			"failed storing latest backfilled blockID to disk: %w, %w",
			err,
			block.ErrInternalBlockBackfilling,
		)
	}
	return nil
}

func (dt *DownloadsTracker) getBlockAtHeight(ctx context.Context, height uint64) (*Block, error) {
	blkID, err := dt.getBlkIDAtHeigth(ctx, height)
	if err != nil {
		return nil, fmt.Errorf(
			"failed retrieving latest backfilled blockID at height %d: %w, %w",
			height,
			err,
			block.ErrInternalBlockBackfilling,
		)
	}
	blk, err := dt.getBlk(ctx, blkID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed retrieving latest backfilled blockID %s: %w, %w",
			blkID,
			err,
			block.ErrInternalBlockBackfilling,
		)
	}
	return blk, nil
}
