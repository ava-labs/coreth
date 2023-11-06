// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	i9n "github.com/ava-labs/coreth/tests/integration"
)

var _ = ginkgo.Describe("[VM] [Atomic TX]", func() {
	ginkgo.It("should support issuing atomic transactions", func() {
		f := i9n.GetFixture()

		key := f.GetPrefundedKey()
		importAmount := uint64(50000000)

		_ = f.IssueImportTx(
			f.GetXChainID(),
			importAmount,
			evm.GetEthAddress(key),
			i9n.InitialBaseFee,
			[]*secp256k1.PrivateKey{
				key,
			},
		)

		_ = f.IssueExportTx(
			f.GetAVAXAssetID(),
			importAmount-(2*params.AvalancheAtomicTxFee),
			f.GetXChainID(),
			key.Address(),
			i9n.InitialBaseFee,
			[]*secp256k1.PrivateKey{
				key,
			},
		)
	})

})
