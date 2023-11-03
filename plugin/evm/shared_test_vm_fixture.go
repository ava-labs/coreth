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

// Arbitrarily large amount of AVAX (10^12) to fund keys on the C-Chain for testing
var DefaultFundedKeyCChainAmount = new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)

type vmFixture struct {
	require      *require.Assertions
	assert       *assert.Assertions
	prefundedKey *secp256k1.PrivateKey
	vm           *VM
	issuer       chan engCommon.Message
	height       uint64
}

func CreateVMFixture(
	t require.TestingT,
) *vmFixture {
	require := require.New(t)

	prefundedKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		prefundedKey.Address(): DefaultFundedKeyCChainAmount.Uint64(),
	})

	return &vmFixture{
		require:      require,
		assert:       assert.New(t),
		prefundedKey: prefundedKey,
		vm:           vm,
		issuer:       issuer,
	}
}

func (v *vmFixture) Teardown() {
	v.require.NoError(v.vm.Shutdown(context.Background()))
}

func (v *vmFixture) AllocateFundedKey() *secp256k1.PrivateKey {
	// This method supports allocation of funded keys for a shared
	// test network, but for the VM instance the fixture is not
	// intended to be shared so its assumed safe to return the same
	// key from every call.
	return v.prefundedKey
}

func (v *vmFixture) GetXChainID() ids.ID { return v.vm.ctx.XChainID }

func (v *vmFixture) GetAVAXAssetID() ids.ID { return v.vm.ctx.AVAXAssetID }

func (v *vmFixture) IssueImportTx(
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

	v.checkAtomicTxIndexing(importTx, height)

	return importTx
}

func (v *vmFixture) IssueExportTx(
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

	v.checkAtomicTxIndexing(exportTx, height)

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

func (v *vmFixture) checkAtomicTxIndexing(tx *Tx, expectedHeight uint64) {
	// Check that the atomic transactions were indexed as expected.
	indexedTx, status, height, err := v.vm.getAtomicTx(tx.ID())
	v.require.NoError(err)
	v.assert.Equal(Accepted, status)
	v.assert.Equal(expectedHeight, height, "unexpected height")
	v.assert.Equal(indexedTx.ID(), tx.ID(), "expected ID of indexed tx to match original txID")
}
