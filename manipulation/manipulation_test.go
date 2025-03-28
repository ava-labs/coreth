package manipulation

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/counter"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/assert"
)

func TestManipulator(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	addr2 := crypto.PubkeyToAddress(key2.PublicKey)
	addr3 := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	addr4 := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")

	chainID := big.NewInt(1)
	testChainConfig := &params.ChainConfig{
		ChainID:             chainID,
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
	}
	tx1 := types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1e9), nil)
	tx1Signed, _ := types.SignTx(tx1, types.NewEIP155Signer(chainID), key1)
	tx2 := types.NewTransaction(0, addr1, big.NewInt(200), 21000, big.NewInt(1e9), nil)
	tx2Signed, _ := types.SignTx(tx2, types.NewEIP155Signer(chainID), key2)
	tx3 := types.NewTransaction(0, addr3, big.NewInt(300), 21000, big.NewInt(1e9), nil)
	tx3Signed, _ := types.SignTx(tx3, types.NewEIP155Signer(chainID), key1)
	tx4 := types.NewTransaction(0, addr4, big.NewInt(400), 21000, big.NewInt(1e9), nil)
	tx4Signed, _ := types.SignTx(tx4, types.NewEIP155Signer(chainID), key2)

	// Test 1: Manipulator
	t.Run("Initialization", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		m := New(true, true, logger, tc, testChainConfig)

		assert.True(t, m.Enabled, "Manipulator should be enabled")
		assert.True(t, m.DetectInjection, "DetectInjection should be enabled")
		assert.NotNil(t, m.CensoredAddresses, "CensoredAddresses should be initialized")
		assert.NotNil(t, m.PriorityAddresses, "PriorityAddresses should be initialized")
		assert.Equal(t, 0, len(m.CensoredAddresses), "CensoredAddresses should be empty initially")
		assert.Equal(t, 0, len(m.PriorityAddresses), "PriorityAddresses should be empty initially")
	})

	// Test 2: Checking censorship
	t.Run("Censorship", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		m := New(true, false, logger, tc, testChainConfig)
		m.AddCensoredAddress(addr1)
		m.AddCensoredAddress(addr3)

		assert.True(t, m.ShouldCensor(tx1Signed), "Transaction from censored sender should be censored")
		assert.True(t, m.ShouldCensor(tx2Signed), "Transaction to censored recipient should be censored")
		assert.True(t, m.ShouldCensor(tx3Signed), "Transaction to censored recipient should be censored")
		assert.False(t, m.ShouldCensor(tx4Signed), "Transaction from non-censored sender to non-censored recipient should not be censored")
		assert.False(t, m.ShouldCensor(tx2), "Unsigned transaction should not be censored due to sender error")
	})

	// Test 3: Checking prioritization
	t.Run("Prioritization", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		m := New(true, false, logger, tc, testChainConfig)
		m.AddPriorityAddress(addr2)

		assert.True(t, m.ShouldPrioritize(tx2Signed), "Transaction from priority address should be prioritized")
		assert.False(t, m.ShouldPrioritize(tx1Signed), "Transaction from non-priority address should not be prioritized")
		assert.False(t, m.ShouldPrioritize(tx3), "Unsigned transaction should not be prioritized due to sender error")
	})

	// Test 4: Checking reordering
	t.Run("Reordering", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		m := New(true, false, logger, tc, testChainConfig)
		m.AddPriorityAddress(addr2)

		txs := []*types.Transaction{tx1Signed, tx2Signed, tx3Signed}
		reordered := m.ApplyReordering(txs)

		assert.Equal(t, 3, len(reordered), "Length of reordered transactions should match input")
		assert.Equal(t, tx2Signed.Hash(), reordered[0].Hash(), "Priority transaction should be first")
	})

	// Test 5: Disabled Manipulator
	t.Run("Disabled", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		m := New(false, true, logger, tc, testChainConfig)
		m.AddCensoredAddress(addr1)
		m.AddPriorityAddress(addr2)

		assert.False(t, m.ShouldCensor(tx1Signed), "Censorship should be disabled")
		assert.False(t, m.ShouldPrioritize(tx2Signed), "Prioritization should be disabled")
		assert.False(t, m.ShouldDropAsInjection(tx1Signed), "Injection detection should be disabled when Manipulator is off")
	})

	// Test 6: Testing InitGlobalConfig
	t.Run("InitGlobalConfig", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		configJSON := `{
			"enabled": true,
			"censored_addresses": ["` + addr1.Hex() + `"],
			"priority_addresses": ["` + addr2.Hex() + `"],
			"detect_injection": true
		}`

		err := InitGlobalConfig(configJSON, logger, tc, testChainConfig)
		assert.NoError(t, err, "InitGlobalConfig should succeed")

		m := GetGlobalManipulator()
		assert.True(t, m.Enabled, "Manipulator should be enabled")
		assert.True(t, m.DetectInjection, "DetectInjection should be enabled")
		assert.Equal(t, 1, len(m.CensoredAddresses), "Should have 1 censored address")
		assert.Equal(t, 1, len(m.PriorityAddresses), "Should have 1 priority address")
		assert.True(t, m.ShouldCensor(tx1Signed), "Censorship should work after init")
		assert.True(t, m.ShouldPrioritize(tx2Signed), "Prioritization should work after init")
	})

	// Test 7: Injections
	t.Run("InjectionDetection", func(t *testing.T) {
		logger := log.Root()
		tc := counter.NewTxCounter()
		m := New(true, true, logger, tc, testChainConfig)

		tc.AddTx(tx1Signed)

		assert.False(t, m.ShouldDropAsInjection(tx1Signed), "Known transaction should not be dropped")
		assert.True(t, m.ShouldDropAsInjection(tx2Signed), "Unknown transaction should be dropped as injection")
	})
}
