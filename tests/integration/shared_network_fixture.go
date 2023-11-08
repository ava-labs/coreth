// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"fmt"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	"github.com/ava-labs/coreth/plugin/evm"
)

// Shared network implementation of IntegrationFixture
type sharedNetworkFixture struct {
	require *require.Assertions

	// The URI of the only node the fixture is intended to communicate with
	nodeURI testnet.NodeURI

	// Prefunded key used to configure the wallet
	prefundedKey *secp256k1.PrivateKey
}

func newSharedNetworkFixture(t require.TestingT) *sharedNetworkFixture {
	return &sharedNetworkFixture{
		require:      require.New(t),
		nodeURI:      e2e.Env.GetRandomNodeURI(),
		prefundedKey: e2e.Env.AllocateFundedKey(),
	}
}

func (f *sharedNetworkFixture) Teardown() {
	if !ginkgo.CurrentSpecReport().Failed() {
		// Only check if bootstrap is possible for passing tests
		e2e.CheckBootstrapIsPossible(e2e.Env.GetNetwork())
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
	toEthAddress ethcommon.Address,
	keys []*secp256k1.PrivateKey,
) *evm.Tx {
	keychain := secp256k1fx.NewKeychain(keys...)
	wallet := e2e.NewWallet(keychain, f.nodeURI)
	ethClient := e2e.NewEthClient(f.nodeURI)
	chainAlias := getChainAlias(chainID)

	ginkgo.By(fmt.Sprintf("exporting AVAX from the %s-Chain to the C-Chain", chainAlias))
	_, err := wallet.X().IssueExportTx(
		wallet.C().BlockchainID(),
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: f.GetAVAXAssetID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     keysToAddresses(keys),
					},
				},
			},
		},
		e2e.WithDefaultContext(),
		e2e.WithSuggestedGasPrice(ethClient),
	)
	f.require.NoError(err)

	ginkgo.By(fmt.Sprintf("importing AVAX from the %s-Chain to the C-Chain", chainAlias))
	tx, err := wallet.C().IssueImportTx(
		chainID,
		toEthAddress,
		e2e.WithDefaultContext(),
	)
	f.require.NoError(err)

	ginkgo.By("checking that the recipient address has received imported funds on the C-Chain")
	balance, err := ethClient.BalanceAt(e2e.DefaultContext(), toEthAddress, nil)
	f.require.NoError(err)
	f.require.Positive(balance.Cmp(big.NewInt(0)))

	return tx
}

func (f *sharedNetworkFixture) IssueExportTx(
	_ ids.ID, // Ignored - wallet will determine correct asset id
	amount uint64,
	chainID ids.ID,
	toAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
) *evm.Tx {
	keychain := secp256k1fx.NewKeychain(keys...)
	wallet := e2e.NewWallet(keychain, f.nodeURI)
	ethClient := e2e.NewEthClient(f.nodeURI)
	chainAlias := getChainAlias(chainID)

	ginkgo.By(fmt.Sprintf("exporting AVAX from the C-Chain to the %s-Chain", chainAlias))
	tx, err := wallet.C().IssueExportTx(
		chainID,
		[]*secp256k1fx.TransferOutput{
			{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     keysToAddresses(keys),
				},
			},
		},
		e2e.WithDefaultContext(),
		e2e.WithSuggestedGasPrice(ethClient),
	)
	f.require.NoError(err)

	ginkgo.By(fmt.Sprintf("importing AVAX from the C-Chain to the %s-Chain", chainAlias))
	_, err = wallet.X().IssueImportTx(
		wallet.C().BlockchainID(),
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				toAddress,
			},
		},
		e2e.WithDefaultContext(),
	)
	f.require.NoError(err)

	ginkgo.By(fmt.Sprintf("checking that the recipient address has received imported funds on the %s-Chain", chainAlias))
	balances, err := wallet.X().Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
		toAddress,
	)))
	f.require.NoError(err)
	f.require.Positive(balances[f.GetAVAXAssetID()])

	return tx
}

func keysToAddresses(keys []*secp256k1.PrivateKey) []ids.ShortID {
	addresses := make([]ids.ShortID, len(keys))
	for i, key := range keys {
		addresses[i] = key.Address()
	}
	return addresses
}

// Determine the chain alias for a chainID representing either the X- or P-Chain.
func getChainAlias(chainID ids.ID) string {
	if chainID == constants.PlatformChainID {
		return "P"
	}
	return "X"
}
