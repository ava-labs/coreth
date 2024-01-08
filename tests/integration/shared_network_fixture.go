// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
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
	nodeURI tmpnet.NodeURI

	// Pre-funded key used to configure the wallet
	preFundedKey *secp256k1.PrivateKey
}

func newSharedNetworkFixture(t require.TestingT) *sharedNetworkFixture {
	return &sharedNetworkFixture{
		require:      require.New(t),
		nodeURI:      e2e.Env.GetRandomNodeURI(),
		preFundedKey: e2e.Env.AllocatePreFundedKey(),
	}
}

func (f *sharedNetworkFixture) Teardown() {
	if !ginkgo.CurrentSpecReport().Failed() {
		// Only check if bootstrap is possible for passing tests
		// TODO(marun) Ensure this is safe to perform on teardown. Currently it uses DeferCleanup to stop the node.
		CheckBootstrapIsPossible(e2e.Env.GetNetwork())

		// TODO(marun) Allow node restart to be skipped just like the bootstrap check (to allow for parallel execution and faster dev iteration)
		ginkgo.By(fmt.Sprintf("checking if restart of %q is possible with current network state", f.nodeURI.NodeID))
		network, err := tmpnet.ReadNetwork(e2e.Env.NetworkDir)
		f.require.NoError(err)
		var targetNode *tmpnet.Node
		for _, node := range network.Nodes {
			if node.NodeID == f.nodeURI.NodeID {
				targetNode = node
				break
			}
		}
		f.require.NotNil(targetNode)

		ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		f.require.NoError(targetNode.Stop(ctx))

		// TODO(marun) Update to use Network.StartNode which handles bootstrap setup automatically once https://github.com/ava-labs/avalanchego/pull/2464 merges
		bootstrapIPs, bootstrapIDs, err := network.GetBootstrapIPsAndIDs()
		f.require.NoError(err)
		f.require.NotEmpty(bootstrapIDs)
		targetNode.Flags[config.BootstrapIDsKey] = strings.Join(bootstrapIDs, ",")
		targetNode.Flags[config.BootstrapIPsKey] = strings.Join(bootstrapIPs, ",")
		f.require.NoError(targetNode.Write())

		f.require.NoError(targetNode.Start(ginkgo.GinkgoWriter))

		ginkgo.By(fmt.Sprintf("waiting for node %q to report healthy after restart", targetNode.NodeID))
		e2e.WaitForHealthy(targetNode)
	}
}

func (f *sharedNetworkFixture) GetPreFundedKey() *secp256k1.PrivateKey {
	return f.preFundedKey
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

// TODO(marun) Remove when e2e.CheckBootstrapIsPossible is available in a tagged release of avalanchego
func CheckBootstrapIsPossible(network *tmpnet.Network) {
	require := require.New(ginkgo.GinkgoT())

	if len(os.Getenv(e2e.SkipBootstrapChecksEnvName)) > 0 {
		tests.Outf("{{yellow}}Skipping bootstrap check due to the %s env var being set", e2e.SkipBootstrapChecksEnvName)
		return
	}
	ginkgo.By("checking if bootstrap is possible with the current network state")

	ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
	defer cancel()

	node, err := network.AddEphemeralNode(ctx, ginkgo.GinkgoWriter, tmpnet.FlagsMap{})
	// AddEphemeralNode will initiate node stop if an error is encountered during start,
	// so no further cleanup effort is required if an error is seen here.
	require.NoError(err)

	// Ensure the node is always stopped at the end of the check
	defer func() {
		ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	}()

	// Check that the node becomes healthy within timeout
	require.NoError(tmpnet.WaitForHealthy(ctx, node))
}
