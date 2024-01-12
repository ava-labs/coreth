// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

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
	preFundedKey *secp256k1.PrivateKey
	vm           *VM
	issuer       chan engCommon.Message
	height       uint64
}

func CreateVMFixture(t require.TestingT) *vmFixture {
	require := require.New(t)

	preFundedKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		preFundedKey.Address(): defaultFundedKeyCChainAmount.Uint64(),
	})

	return &vmFixture{
		require:      require,
		preFundedKey: preFundedKey,
		vm:           vm,
		issuer:       issuer,
	}
}

func (v *vmFixture) Teardown() {
	v.require.NoError(v.vm.Shutdown(context.Background()))
}

func (v *vmFixture) GetPreFundedKey() *secp256k1.PrivateKey {
	return v.preFundedKey
}

func (v *vmFixture) GetXChainID() ids.ID { return v.vm.ctx.XChainID }

func (v *vmFixture) GetAVAXAssetID() ids.ID { return v.vm.ctx.AVAXAssetID }

func (v *vmFixture) IssueImportTx(
	chainID ids.ID,
	_ uint64, // amount is ignored - all available funds will be imported
	to common.Address,
	keys []*secp256k1.PrivateKey,
) *Tx {
	ginkgo.By(fmt.Sprintf("importing AVAX from the %s-Chain to the C-Chain", v.getChainAlias(chainID)))

	importTx, err := v.vm.newImportTx(chainID, to, initialBaseFee, keys)
	v.require.NoError(err)

	v.require.NoError(v.vm.mempool.AddLocalTx(importTx))

	<-v.issuer

	v.buildAndAcceptBlockForTx(context.Background(), importTx)

	return importTx
}

func (v *vmFixture) IssueExportTx(
	assetID ids.ID,
	amount uint64,
	chainID ids.ID,
	to ids.ShortID,
	keys []*secp256k1.PrivateKey,
) *Tx {
	ginkgo.By(fmt.Sprintf("exporting AVAX from the C-Chain to the %s-Chain", v.getChainAlias(chainID)))

	exportTx, err := v.vm.newExportTx(assetID, amount, chainID, to, initialBaseFee, keys)
	v.require.NoError(err)

	v.require.NoError(v.vm.mempool.AddLocalTx(exportTx))

	<-v.issuer

	v.buildAndAcceptBlockForTx(context.Background(), exportTx)

	return exportTx
}

func (v *vmFixture) buildAndAcceptBlockForTx(ctx context.Context, tx *Tx) {
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

	v.checkAtomicTxIndexing(tx, v.height)
}

func (v *vmFixture) checkAtomicTxIndexing(tx *Tx, expectedHeight uint64) {
	// Check that the atomic transactions were indexed as expected.
	indexedTx, status, height, err := v.vm.getAtomicTx(tx.ID())
	v.require.NoError(err)
	v.require.Equal(Accepted, status)
	v.require.Equal(expectedHeight, height, "unexpected height")
	v.require.Equal(indexedTx.ID(), tx.ID(), "expected ID of indexed tx to match original txID")
}

// Determine the chain alias for a chainID representing either the X- or P-Chain.
func (v *vmFixture) getChainAlias(chainID ids.ID) string {
	if chainID == v.GetXChainID() {
		return "X"
	}
	return "P"
}
