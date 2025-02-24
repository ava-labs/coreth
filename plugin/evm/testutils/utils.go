package testutils

import (
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"

	"github.com/ethereum/go-ethereum/common"
)

var (
	TestKeys         []*secp256k1.PrivateKey
	TestEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	TestShortIDAddrs []ids.ShortID
	InitialBaseFee   = big.NewInt(ap3.InitialBaseFee)

	keys = []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	}
)

func init() {
	var b []byte
	for _, key := range keys {
		b, _ = cb58.Decode(key)
		pk, _ := secp256k1.ToPrivateKey(b)
		TestKeys = append(TestKeys, pk)
		TestEthAddrs = append(TestEthAddrs, pk.EthAddress())
		TestShortIDAddrs = append(TestShortIDAddrs, pk.Address())
	}
}
