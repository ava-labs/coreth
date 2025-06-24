// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	wallet "github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/libevm/log"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
)

type SubnetSuite struct {
	blockchainIDs map[string]string
	lock          sync.RWMutex
}

func (s *SubnetSuite) GetBlockchainID(alias string) string {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.blockchainIDs[alias]
}

func (s *SubnetSuite) SetBlockchainIDs(blockchainIDs map[string]string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.blockchainIDs = blockchainIDs
}

// CreateNewSubnet creates a new subnet and subnet-evm blockchain with the given genesis file.
// returns the ID of the new created blockchain.
func CreateNewSubnet(ctx context.Context, genesisFilePath string) string {
	require := require.New(ginkgo.GinkgoT())

	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	// MakeWallet fetches the available UTXOs owned by [kc] on the network
	// that [LocalAPIURI] is hosting.
	wallet, err := wallet.MakeWallet(ctx, DefaultLocalNodeURI, kc, kc, wallet.WalletConfig{})
	require.NoError(err)

	pWallet := wallet.P()

	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			genesis.EWOQKey.PublicKey().Address(),
		},
	}

	wd, err := os.Getwd()
	require.NoError(err)
	log.Info("Reading genesis file", "filePath", genesisFilePath, "wd", wd)
	genesisBytes, err := os.ReadFile(genesisFilePath)
	require.NoError(err)

	log.Info("Creating new subnet")
	createSubnetTx, err := pWallet.IssueCreateSubnetTx(owner)
	require.NoError(err)

	genesis := &core.Genesis{}
	require.NoError(json.Unmarshal(genesisBytes, genesis))

	log.Info("Creating new subnet-evm blockchain", "genesis", genesis)
	createChainTx, err := pWallet.IssueCreateChainTx(
		createSubnetTx.ID(),
		genesisBytes,
		subnetVMID,
		nil,
		"testChain",
	)
	require.NoError(err)
	createChainTxID := createChainTx.ID()

	// Confirm the new blockchain is ready by waiting for the readiness endpoint
	infoClient := info.NewClient(DefaultLocalNodeURI)
	bootstrapped, err := info.AwaitBootstrapped(ctx, infoClient, createChainTxID.String(), 2*time.Second)
	require.NoError(err)
	require.True(bootstrapped)

	// Return the blockchainID of the newly created blockchain
	return createChainTxID.String()
}

// GetDefaultChainURI returns the default chain URI for a given blockchainID
func GetDefaultChainURI(blockchainID string) string {
	return fmt.Sprintf("%s/ext/bc/%s/rpc", DefaultLocalNodeURI, blockchainID)
}

// GetFilesAndAliases returns a map of aliases to file paths in given [dir].
func GetFilesAndAliases(dir string) (map[string]string, error) {
	files, err := filepath.Glob(dir)
	if err != nil {
		return nil, err
	}
	aliasesToFiles := make(map[string]string)
	for _, file := range files {
		alias := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		aliasesToFiles[alias] = file
	}
	return aliasesToFiles, nil
}
