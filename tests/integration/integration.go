// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/params"
)

var (
	InitialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)

	// Will be initialized by BeforeSuite
	// TODO(marun) Maybe supply with a test env as per avalanchego's example?
	TestKeys         []*secp256k1.PrivateKey
	TestEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	TestShortIDAddrs []ids.ShortID
)
