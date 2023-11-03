// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/eth/filters"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	i9n "github.com/ava-labs/coreth/tests/integration"
)

var _ = ginkgo.Describe("[VM] [Atomic TX]", func() {
	require := require.New(ginkgo.GinkgoT())
	assert := assert.New(ginkgo.GinkgoT())

	ginkgo.It("should support issuing atomic transactions", func() {
		importAmount := uint64(50000000)
		fixture := evm.CreateVMFixture(ginkgo.GinkgoT(), map[ids.ShortID]uint64{
			i9n.TestShortIDAddrs[0]: importAmount,
		})

		ginkgo.DeferCleanup(func() {
			fixture.Teardown()
		})

		// TODO: Construct the atomic transaction
		// fixture.BuildAndAccept()
		// Construct atomic transaction
		// build and accept
		// check some indices

		vm := fixture.VM

		importTx, err := vm.NewImportTx(
			vm.GetXChainID(),
			i9n.TestEthAddrs[0],
			i9n.InitialBaseFee,
			[]*secp256k1.PrivateKey{i9n.TestKeys[0]},
		)
		require.NoError(err)

		require.NoError(vm.GetAtomicMempool().AddLocalTx(importTx))

		<-fixture.Issuer

		blk, err := vm.BuildBlock(context.Background())
		require.NoError(err)

		require.NoError(blk.Verify(context.Background()))
		require.Equal(blk.Status(), choices.Processing)

		require.NoError(vm.SetPreference(context.Background(), blk.ID()))

		err = blk.Accept(context.Background())
		require.NoError(err)
		require.Equal(blk.Status(), choices.Accepted)

		lastAcceptedID, err := vm.LastAccepted(context.Background())
		require.NoError(err)
		require.Equal(lastAcceptedID, blk.ID())
		vm.GetBlockChain().DrainAcceptorQueue()
		filterAPI := filters.NewFilterAPI(filters.NewFilterSystem(vm.GetAPIBackend(), filters.Config{
			Timeout: 5 * time.Minute,
		}))
		blockHash := common.Hash(blk.ID())
		logs, err := filterAPI.GetLogs(context.Background(), filters.FilterCriteria{
			BlockHash: &blockHash,
		})
		require.NoError(err)
		require.Zero(len(logs))

		exportTx, err := vm.NewExportTx(
			vm.GetAVAXAssetID(),
			importAmount-(2*params.AvalancheAtomicTxFee),
			vm.GetXChainID(),
			i9n.TestShortIDAddrs[0],
			i9n.InitialBaseFee,
			[]*secp256k1.PrivateKey{
				i9n.TestKeys[0],
			},
		)
		require.NoError(err)

		require.NoError(vm.GetAtomicMempool().AddLocalTx(exportTx))

		<-fixture.Issuer

		blk2, err := vm.BuildBlock(context.Background())
		require.NoError(err)

		require.NoError(blk2.Verify(context.Background()))
		require.Equal(blk2.Status(), choices.Processing)

		require.NoError(blk2.Accept(context.Background()))
		require.Equal(blk2.Status(), choices.Accepted)

		lastAcceptedID, err = vm.LastAccepted(context.Background())
		require.NoError(err)
		require.Equal(lastAcceptedID, blk2.ID())

		// Check that both atomic transactions were indexed as expected.
		indexedImportTx, status, height, err := vm.GetAtomicTx(importTx.ID())
		require.NoError(err)
		assert.Equal(evm.Accepted, status)
		assert.Equal(uint64(1), height, "expected height of indexed import tx to be 1")
		assert.Equal(indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

		indexedExportTx, status, height, err := vm.GetAtomicTx(exportTx.ID())
		require.NoError(err)
		assert.Equal(evm.Accepted, status)
		assert.Equal(uint64(2), height, "expected height of indexed export tx to be 2")
		assert.Equal(indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
	})

})
