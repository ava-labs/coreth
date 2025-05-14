// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"

	"github.com/ava-labs/libevm/common"
)

var (
	TestKeys         = secp256k1.TestKeys()[:3]
	TestEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	TestShortIDAddrs []ids.ShortID
	InitialBaseFee   = big.NewInt(ap3.InitialBaseFee)
)

func init() {
	for _, pk := range TestKeys {
		TestEthAddrs = append(TestEthAddrs, pk.EthAddress())
		TestShortIDAddrs = append(TestShortIDAddrs, pk.Address())
	}
}
