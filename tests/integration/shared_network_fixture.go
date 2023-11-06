// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	"github.com/ava-labs/coreth/plugin/evm"
)

// Shared network implementation of IntegrationFixture
type sharedNetworkFixture struct {
	// The URI of the only node the fixture is intended to communciate with
	nodeURI testnet.NodeURI

	// Wallet to perform transactions
	baseWallet primary.Wallet

	// Prefunded key used to configure the wallet
	prefundedKey *secp256k1.PrivateKey

	require *require.Assertions
}

func newSharedNetworkFixture(t require.TestingT) *sharedNetworkFixture {
	nodeURI := e2e.Env.GetRandomNodeURI()
	keychain := e2e.Env.NewKeychain(1)
	return &sharedNetworkFixture{
		nodeURI:      nodeURI,
		baseWallet:   e2e.NewWallet(keychain, nodeURI),
		prefundedKey: keychain.Keys[0],
		require:      require.New(t),
	}
}

func (f *sharedNetworkFixture) GetPrefundedKey() *secp256k1.PrivateKey {
	return f.prefundedKey
}

func (f *sharedNetworkFixture) GetXChainID() ids.ID {
	id, err := info.NewClient(f.nodeURI.URI).GetBlockchainID(e2e.DefaultContext(), "X")
	f.require.NoError(err)
	return id
}

func (f *sharedNetworkFixture) GetAVAXAssetID() ids.ID {
	asset, err := avm.NewClient(f.nodeURI.URI, "X").GetAssetDescription(e2e.DefaultContext(), "AVAX")
	f.require.NoError(err)
	return asset.AssetID
}

func (f *sharedNetworkFixture) IssueImportTx(
	chainID ids.ID,
	amount uint64,
	to ethcommon.Address,
	baseFee *big.Int,
	keys []*secp256k1.PrivateKey,
) *evm.Tx {
	addresses := make([]ids.ShortID, len(keys))
	for i, key := range keys {
		addresses[i] = key.Address()
	}

	// TODO(marun) Ensure this message accurately reflects the recipient chain
	ginkgo.By("exporting AVAX from the X-Chain to the C-Chain")
	_, err := f.baseWallet.X().IssueExportTx(
		f.baseWallet.C().BlockchainID(),
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: f.GetAVAXAssetID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     addresses,
					},
				},
			},
		},
		e2e.WithDefaultContext(),
	)
	f.require.NoError(err)

	// TODO(marun) Ensure this message accurately reflects the recipient chain
	ginkgo.By("importing AVAX from the X-Chain to the C-Chain")
	tx, err := f.baseWallet.C().IssueImportTx(
		chainID,
		to,
		e2e.WithDefaultContext(),
		common.WithBaseFee(baseFee),
	)
	f.require.NoError(err)

	// TODO(marun) verify result

	return tx
}

func (f *sharedNetworkFixture) IssueExportTx(
	_ ids.ID, // Ignored - wallet will determine correct asset id
	amount uint64,
	chainID ids.ID,
	to ids.ShortID,
	baseFee *big.Int,
	keys []*secp256k1.PrivateKey, // Ignored - prefunded key will be used
) *evm.Tx {
	// TODO(marun) Ensure this message accurately reflects the recipient chain
	ginkgo.By("exporting AVAX from the C-Chain to the X-Chain")
	tx, err := f.baseWallet.C().IssueExportTx(
		chainID,
		[]*secp256k1fx.TransferOutput{
			{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						f.prefundedKey.Address(),
					},
				},
			},
		},
		e2e.WithDefaultContext(),
		common.WithBaseFee(baseFee),
	)
	f.require.NoError(err)

	// TODO(marun) Ensure this message accurately reflects the recipient chain
	ginkgo.By("importing AVAX from the C-Chain to the X-Chain")
	_, err = f.baseWallet.X().IssueImportTx(
		f.baseWallet.C().BlockchainID(),
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				to,
			},
		},
		e2e.WithDefaultContext(),
	)
	f.require.NoError(err)

	// TODO(marun) verify result

	return tx
}
