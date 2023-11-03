// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
)

var (
	InitialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
)

type IntegrationFixture interface {
	AllocateFundedKey() *secp256k1.PrivateKey

	GetXChainID() ids.ID

	GetAVAXAssetID() ids.ID

	IssueImportTx(
		ctx context.Context,
		chainID ids.ID,
		to common.Address,
		baseFee *big.Int,
		keys []*secp256k1.PrivateKey,
	) *evm.Tx

	IssueExportTx(
		ctx context.Context,
		assetID ids.ID,
		amount uint64,
		chainID ids.ID,
		to ids.ShortID,
		baseFee *big.Int,
		keys []*secp256k1.PrivateKey,
	) *evm.Tx

	Teardown()
}

func NewIntegrationFixture() IntegrationFixture {
	// TODO(marun) Support use of testnet fixture

	vmFixture := evm.CreateVMFixture(ginkgo.GinkgoT())
	ginkgo.DeferCleanup(func() {
		vmFixture.Teardown()
	})

	return vmFixture
}
