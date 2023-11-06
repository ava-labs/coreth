// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"flag"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/coreth/tests/integration/vm"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Coreth EVM Integration Tests")
}

var (
	flagVars *e2e.FlagVars

	useNetworkFixture bool
)

func init() {
	flagVars = e2e.RegisterFlags()
	flag.BoolVar(
		&useNetworkFixture,
		"use-network-fixture",
		false,
		"[optional] whether to target a network fixture. By default a lighter-weight vm fixture will be used.",
	)
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	if useNetworkFixture {
		// Run only once in the first ginkgo process
		return e2e.NewTestEnvironment(flagVars).Marshal()
	}
	return nil
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	if len(envBytes) > 0 {
		e2e.InitSharedTestEnvironment(envBytes)
	}
})
