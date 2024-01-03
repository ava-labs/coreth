// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/ava-labs/coreth/plugin/evm"
	i9n "github.com/ava-labs/coreth/tests/integration"
)

var _ = ginkgo.Describe("[VM] [Atomic TX]", func() {
	require := require.New(ginkgo.GinkgoT())

	// Arbitrary amount to transfer
	const importAmount = 100 * units.Avax
	const exportAmount = 80 * units.Avax // Less than import to account for fees

	ginkgo.It("should support issuing atomic transactions", func() {
		f := i9n.GetFixture()

		preFundedKey := f.GetPreFundedKey()

		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		recipientEthAddress := evm.GetEthAddress(recipientKey)
		tests.Outf("{{blue}} using recipient address: %+v{{/}}\n", recipientEthAddress)

		_ = f.IssueImportTx(
			f.GetXChainID(),
			importAmount,
			recipientEthAddress,
			[]*secp256k1.PrivateKey{
				preFundedKey,
			},
		)

		_ = f.IssueExportTx(
			f.GetAVAXAssetID(),
			exportAmount,
			f.GetXChainID(),
			recipientKey.Address(),
			[]*secp256k1.PrivateKey{
				recipientKey,
			},
		)
	})

})
