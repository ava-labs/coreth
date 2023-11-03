// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	engCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/eth/filters"
)

type vmFixture struct {
	require *require.Assertions
	issuer  chan engCommon.Message
	height  uint64
	vm      *VM
}

func CreateVMFixture(t require.TestingT, prefundedUTXOs map[ids.ShortID]uint64) *vmFixture {
	require := require.New(t)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", prefundedUTXOs)
	return &vmFixture{
		vm:      vm,
		issuer:  issuer,
		require: require,
	}
}

func (v *vmFixture) Teardown() {
	v.require.NoError(v.vm.Shutdown(context.Background()))
}

func (v *vmFixture) GetXChainID() ids.ID { return v.vm.ctx.XChainID }

func (v *vmFixture) GetAVAXAssetID() ids.ID { return v.vm.ctx.AVAXAssetID }

func (v *vmFixture) IssueImportTx(
	t require.TestingT,
	ctx context.Context,
	chainID ids.ID,
	to common.Address,
	baseFee *big.Int,
	keys []*secp256k1.PrivateKey,
) *Tx {
	importTx, err := v.vm.newImportTx(chainID, to, baseFee, keys)
	v.require.NoError(err)

	v.require.NoError(v.vm.mempool.AddLocalTx(importTx))

	<-v.issuer

	height := v.buildAndAcceptBlockForTx(ctx, importTx)

	v.checkAtomicTxIndexing(t, importTx, height)

	return importTx
}

func (v *vmFixture) IssueExportTx(
	t require.TestingT,
	ctx context.Context,
	assetID ids.ID,
	amount uint64,
	chainID ids.ID,
	to ids.ShortID,
	baseFee *big.Int,
	keys []*secp256k1.PrivateKey,
) *Tx {
	exportTx, err := v.vm.newExportTx(assetID, amount, chainID, to, baseFee, keys)
	v.require.NoError(err)

	v.require.NoError(v.vm.mempool.AddLocalTx(exportTx))

	<-v.issuer

	height := v.buildAndAcceptBlockForTx(ctx, exportTx)

	v.checkAtomicTxIndexing(t, exportTx, height)

	return exportTx
}

func (v *vmFixture) buildAndAcceptBlockForTx(ctx context.Context, tx *Tx) uint64 {
	require := v.require

	blk, err := v.vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk.Verify(ctx))
	require.Equal(blk.Status(), choices.Processing)

	require.NoError(v.vm.SetPreference(ctx, blk.ID()))

	require.NoError(blk.Accept(ctx))
	require.Equal(blk.Status(), choices.Accepted)

	lastAcceptedID, err := v.vm.LastAccepted(ctx)
	require.NoError(err)
	require.Equal(lastAcceptedID, blk.ID())

	v.vm.blockChain.DrainAcceptorQueue()
	filterAPI := filters.NewFilterAPI(filters.NewFilterSystem(v.vm.eth.APIBackend, filters.Config{
		Timeout: 5 * time.Minute,
	}))
	blockHash := common.Hash(blk.ID())
	logs, err := filterAPI.GetLogs(ctx, filters.FilterCriteria{
		BlockHash: &blockHash,
	})
	require.NoError(err)
	require.Zero(len(logs))

	v.height += uint64(1)
	return v.height
}

func (v *vmFixture) checkAtomicTxIndexing(t require.TestingT, tx *Tx, expectedHeight uint64) {
	// Check that the atomic transactions were indexed as expected.
	indexedTx, status, height, err := v.vm.getAtomicTx(tx.ID())
	v.require.NoError(err)
	assert.Equal(t, Accepted, status)
	assert.Equal(t, expectedHeight, height, "unexpected height")
	assert.Equal(t, indexedTx.ID(), tx.ID(), "expected ID of indexed tx to match original txID")
}
