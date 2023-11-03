// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	i9n "github.com/ava-labs/coreth/tests/integration"
)

var _ = ginkgo.Describe("[VM] [Atomic TX]", func() {
	ginkgo.It("should support issuing atomic transactions", func() {
		importAmount := uint64(50000000)

		vmFixture := evm.CreateVMFixture(ginkgo.GinkgoT(), map[ids.ShortID]uint64{
			i9n.TestShortIDAddrs[0]: importAmount,
		})
		ginkgo.DeferCleanup(func() {
			vmFixture.Teardown()
		})

		_ = vmFixture.IssueImportTx(
			ginkgo.GinkgoT(),
			context.Background(),
			vmFixture.GetXChainID(),
			i9n.TestEthAddrs[0],
			i9n.InitialBaseFee,
			[]*secp256k1.PrivateKey{i9n.TestKeys[0]},
		)

		_ = vmFixture.IssueExportTx(
			ginkgo.GinkgoT(),
			context.Background(),
			vmFixture.GetAVAXAssetID(),
			importAmount-(2*params.AvalancheAtomicTxFee),
			vmFixture.GetXChainID(),
			i9n.TestShortIDAddrs[0],
			i9n.InitialBaseFee,
			[]*secp256k1.PrivateKey{
				i9n.TestKeys[0],
			},
		)
	})

})
