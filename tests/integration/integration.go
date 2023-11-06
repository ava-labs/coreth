// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
)

var (
	InitialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
)

type IntegrationFixture interface {
	GetPrefundedKey() *secp256k1.PrivateKey

	GetXChainID() ids.ID

	GetAVAXAssetID() ids.ID

	IssueImportTx(
		chainID ids.ID,
		amount uint64,
		to common.Address,
		baseFee *big.Int,
		keys []*secp256k1.PrivateKey,
	) *evm.Tx

	IssueExportTx(
		assetID ids.ID,
		amount uint64,
		chainID ids.ID,
		to ids.ShortID,
		baseFee *big.Int,
		keys []*secp256k1.PrivateKey,
	) *evm.Tx
}

func GetFixture() IntegrationFixture {
	if e2e.Env == nil {
		// Shared network fixture not initialized, return a fresh vm fixture
		vmFixture := evm.CreateVMFixture(ginkgo.GinkgoT())
		ginkgo.DeferCleanup(func() {
			vmFixture.Teardown()
		})
		return vmFixture
	}

	// e2e.Env being non-nil indicates the use of a shared network fixture
	return newSharedNetworkFixture(ginkgo.GinkgoT())
}
