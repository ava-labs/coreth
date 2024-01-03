// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/plugin/evm"
)

type IntegrationFixture interface {
	Teardown()

	GetPreFundedKey() *secp256k1.PrivateKey

	GetXChainID() ids.ID

	GetAVAXAssetID() ids.ID

	IssueImportTx(
		chainID ids.ID,
		amount uint64,
		to common.Address,
		keys []*secp256k1.PrivateKey,
	) *evm.Tx

	IssueExportTx(
		assetID ids.ID,
		amount uint64,
		chainID ids.ID,
		to ids.ShortID,
		keys []*secp256k1.PrivateKey,
	) *evm.Tx
}

func GetFixture() IntegrationFixture {
	var fixture IntegrationFixture
	if e2e.Env == nil {
		// Shared network fixture not initialized, return a vm fixture
		fixture = evm.CreateVMFixture(ginkgo.GinkgoT())
	} else {
		// e2e.Env being non-nil indicates availability of shared network fixture
		fixture = newSharedNetworkFixture(ginkgo.GinkgoT())
	}
	ginkgo.DeferCleanup(func() {
		fixture.Teardown()
	})

	return fixture
}
