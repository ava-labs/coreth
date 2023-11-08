// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	engCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/eth/filters"
)

var (
	// Arbitrarily large amount of AVAX (10^12) to fund keys on the C-Chain for testing
	defaultFundedKeyCChainAmount = new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
)

type vmFixture struct {
	require      *require.Assertions
	assert       *assert.Assertions
	prefundedKey *secp256k1.PrivateKey
	vm           *VM
	issuer       chan engCommon.Message
	height       uint64
}

func CreateVMFixture(t require.TestingT) *vmFixture {
	require := require.New(t)

	prefundedKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		prefundedKey.Address(): defaultFundedKeyCChainAmount.Uint64(),
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

func (v *vmFixture) GetPrefundedKey() *secp256k1.PrivateKey {
	return v.prefundedKey
}

func (v *vmFixture) GetXChainID() ids.ID { return v.vm.ctx.XChainID }

func (v *vmFixture) GetAVAXAssetID() ids.ID { return v.vm.ctx.AVAXAssetID }

func (v *vmFixture) IssueImportTx(
	chainID ids.ID,
	_ uint64, // amount is ignored - all available funds will be imported
	to common.Address,
	keys []*secp256k1.PrivateKey,
) *Tx {
	// TODO(marun) Ensure this message accurately reflects the sending chain
	ginkgo.By("importing AVAX from the X-Chain to the C-Chain")

	importTx, err := v.vm.newImportTx(chainID, to, initialBaseFee, keys)
	v.require.NoError(err)

	v.require.NoError(v.vm.mempool.AddLocalTx(importTx))

	<-v.issuer

	height := v.buildAndAcceptBlockForTx(context.Background(), importTx)

	v.checkAtomicTxIndexing(importTx, height)

	return importTx
}

func (v *vmFixture) IssueExportTx(
	assetID ids.ID,
	amount uint64,
	chainID ids.ID,
	to ids.ShortID,
	keys []*secp256k1.PrivateKey,
) *Tx {
	// TODO(marun) Ensure this message accurately reflects the recipient chain
	ginkgo.By("exporting AVAX from the C-Chain to the X-Chain")

	exportTx, err := v.vm.newExportTx(assetID, amount, chainID, to, initialBaseFee, keys)
	v.require.NoError(err)

	v.require.NoError(v.vm.mempool.AddLocalTx(exportTx))

	<-v.issuer

	height := v.buildAndAcceptBlockForTx(context.Background(), exportTx)

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
