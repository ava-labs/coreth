// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/filters"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ethereum/go-ethereum/common"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	genesisJSONApricotPhase2 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	testKeys                 []*secp256k1.PrivateKey
	testEthAddrs             []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testShortIDAddrs         []ids.ShortID
	initialBaseFee                              = big.NewInt(params.ApricotPhase3InitialBaseFee)
)

func init() {

}

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
		testKeys = append(testKeys, pk)
		testEthAddrs = append(testEthAddrs, evm.GetEthAddress(pk))
		testShortIDAddrs = append(testShortIDAddrs, pk.PublicKey().Address())
	}
})

var _ = ginkgo.AfterSuite(func() {
	// TODO
})


type IntegrationFixture interface {
	IssueTxs(txs []*types.Transaction)
	IssueAtomicTxs(atomicTxs []*evm.Tx)

	BuildAndAccept()

	Teardown()
	// ConfirmTxs(txs []*types.Transaction)
	// ConfirmAtomicTxs(atomicTxs []*evm.Tx)

	// Define index getter functions
	// GetTxReceipt(txHash common.Hash) *types.Receipt
}

func TestIssueAtomicTxs(t *testing.T) {
	importAmount := uint64(50000000)
	fixture := createVMFixture("", "", nil, map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	ginkgo.DeferCleanup(func() {
		fixture.Teardown()
	})

	// TODO: Construct the atomic transaction
	fixture.BuildAndAccept()
	// Construct atomic transaction
	// build and accept
	// check some indices

	importTx, err := fixture.vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
	vm.blockChain.DrainAcceptorQueue()
	filterAPI := filters.NewFilterAPI(filters.NewFilterSystem(vm.eth.APIBackend, filters.Config{
		Timeout: 5 * time.Minute,
	}))
	blockHash := common.Hash(blk.ID())
	logs, err := filterAPI.GetLogs(context.Background(), filters.FilterCriteria{
		BlockHash: &blockHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("Expected log length to be 0, but found %d", len(logs))
	}
	if logs == nil {
		t.Fatal("Expected logs to be non-nil")
	}

	exportTx, err := vm.newExportTx(vm.ctx.AVAXAssetID, importAmount-(2*params.AvalancheAtomicTxFee), vm.ctx.XChainID, testShortIDAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(exportTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if status := blk2.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blk2.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if status := blk2.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	// Check that both atomic transactions were indexed as expected.
	indexedImportTx, status, height, err := vm.getAtomicTx(importTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, evm.Accepted, status)
	assert.Equal(t, uint64(1), height, "expected height of indexed import tx to be 1")
	assert.Equal(t, indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

	indexedExportTx, status, height, err := vm.getAtomicTx(exportTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, evm.Accepted, status)
	assert.Equal(t, uint64(2), height, "expected height of indexed export tx to be 2")
	assert.Equal(t, indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
}
