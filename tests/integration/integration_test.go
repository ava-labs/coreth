// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	"github.com/ava-labs/coreth/plugin/evm"
	i9n "github.com/ava-labs/coreth/tests/integration"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/coreth/tests/integration/vm"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Coreth EVM Integration Tests")
}

var _ = ginkgo.BeforeSuite(func() {
	var b []byte

	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		b, _ = cb58.Decode(key)
		pk, _ := secp256k1.ToPrivateKey(b)
		i9n.TestKeys = append(i9n.TestKeys, pk)
		i9n.TestEthAddrs = append(i9n.TestEthAddrs, evm.GetEthAddress(pk))
		i9n.TestShortIDAddrs = append(i9n.TestShortIDAddrs, pk.PublicKey().Address())
	}
})
