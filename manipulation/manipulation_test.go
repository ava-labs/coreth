package manipulation_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/manipulation"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestManipulatorInitialization(t *testing.T) {
	logger := logging.NoLog{}
	_, ethAddresses := setupTestWallets(t)
	blockedAddresses := ethAddresses[:2]
	priorityAddress := ethAddresses[2]

	configJSON := `{
		"enabled": true,
		"censored_addresses": ["` + blockedAddresses[0].Hex() + `", "` + blockedAddresses[1].Hex() + `"],
		"priority_addresses": ["` + priorityAddress.Hex() + `"],
		"detect_injection": true
	}`
	if err := manipulation.InitGlobalConfig(configJSON, logger); err != nil {
		t.Fatalf("failed to initialize manipulator: %v", err)
	}

	m := manipulation.GetGlobalManipulator()
	if !m.Enabled {
		t.Errorf("expected Enabled to be true, got false")
	}
	if !m.DetectInjection {
		t.Errorf("expected DetectInjection to be true, got false")
	}
	if m.CensoredAddresses == nil {
		t.Errorf("expected CensoredAddresses to be non-nil")
	}
	if m.PriorityAddresses == nil {
		t.Errorf("expected PriorityAddresses to be non-nil")
	}
	if len(m.CensoredAddresses) != 2 {
		t.Errorf("expected 2 censored addresses, got %d", len(m.CensoredAddresses))
	}
	if len(m.PriorityAddresses) != 1 {
		t.Errorf("expected 1 priority address, got %d", len(m.PriorityAddresses))
	}
}

func TestManipulatorCensorship(t *testing.T) {
	logger := logging.NoLog{}
	wallets, ethAddresses := setupTestWallets(t)
	blockedAddresses := ethAddresses[:2]

	configJSON := `{
		"enabled": true,
		"censored_addresses": ["` + blockedAddresses[0].Hex() + `", "` + blockedAddresses[1].Hex() + `"],
		"priority_addresses": [],
		"detect_injection": false
	}`
	if err := manipulation.InitGlobalConfig(configJSON, logger); err != nil {
		t.Fatalf("failed to initialize manipulator: %v", err)
	}

	m := manipulation.GetGlobalManipulator()
	for i, key := range wallets {
		tx := newTestTx(t, key, ethAddresses[i], big.NewInt(1000))
		if i < 2 {
			if !m.ShouldCensor(tx) {
				t.Errorf("expected transaction from blocked address %s to be censored", ethAddresses[i].Hex())
			}
		} else {
			if m.ShouldCensor(tx) {
				t.Errorf("expected transaction from non-blocked address %s not to be censored", ethAddresses[i].Hex())
			}
		}
	}

	txUnsigned := newTestTx(t, nil, ethAddresses[0], big.NewInt(1000))
	if m.ShouldCensor(txUnsigned) {
		t.Errorf("expected unsigned transaction not to be censored due to sender error")
	}
}

func TestManipulatorPriority(t *testing.T) {
	logger := logging.NoLog{}
	wallets, ethAddresses := setupTestWallets(t)
	priorityAddress := ethAddresses[2]

	configJSON := `{
		"enabled": true,
		"censored_addresses": [],
		"priority_addresses": ["` + priorityAddress.Hex() + `"],
		"detect_injection": false
	}`
	if err := manipulation.InitGlobalConfig(configJSON, logger); err != nil {
		t.Fatalf("failed to initialize manipulator: %v", err)
	}

	m := manipulation.GetGlobalManipulator()
	txPriority := newTestTx(t, wallets[2], ethAddresses[2], big.NewInt(1000))
	tx1 := newTestTx(t, wallets[0], ethAddresses[0], big.NewInt(1000))
	tx2 := newTestTx(t, wallets[1], ethAddresses[1], big.NewInt(1000))

	if !m.ShouldPrioritize(txPriority) {
		t.Errorf("expected transaction from priority address %s to be prioritized", ethAddresses[2].Hex())
	}
	if m.ShouldPrioritize(tx1) {
		t.Errorf("expected transaction from non-priority address %s not to be prioritized", ethAddresses[0].Hex())
	}
	if m.ShouldPrioritize(tx2) {
		t.Errorf("expected transaction from non-priority address %s not to be prioritized", ethAddresses[1].Hex())
	}

	txUnsigned := newTestTx(t, nil, ethAddresses[2], big.NewInt(1000))
	if m.ShouldPrioritize(txUnsigned) {
		t.Errorf("expected unsigned transaction not to be prioritized due to sender error")
	}
}

func TestManipulatorInjectionDetection(t *testing.T) {
	logger := logging.NoLog{}
	wallets, ethAddresses := setupTestWallets(t)

	configJSON := `{
		"enabled": true,
		"censored_addresses": [],
		"priority_addresses": [],
		"detect_injection": true
	}`
	if err := manipulation.InitGlobalConfig(configJSON, logger); err != nil {
		t.Fatalf("failed to initialize manipulator: %v", err)
	}

	m := manipulation.GetGlobalManipulator()
	tx := newTestTx(t, wallets[0], ethAddresses[0], big.NewInt(1000))
	if m.ShouldDropAsInjection(tx) {
		t.Errorf("expected ShouldDropAsInjection to return false, got true")
	}

	txUnsigned := newTestTx(t, nil, ethAddresses[0], big.NewInt(1000))
	if m.ShouldDropAsInjection(txUnsigned) {
		t.Errorf("expected ShouldDropAsInjection to return false for unsigned tx, got true")
	}
}

func TestManipulatorDisabled(t *testing.T) {
	logger := logging.NoLog{}
	wallets, ethAddresses := setupTestWallets(t)

	configJSON := `{
		"enabled": false,
		"censored_addresses": ["` + ethAddresses[0].Hex() + `"],
		"priority_addresses": ["` + ethAddresses[0].Hex() + `"],
		"detect_injection": true
	}`
	if err := manipulation.InitGlobalConfig(configJSON, logger); err != nil {
		t.Fatalf("failed to initialize manipulator: %v", err)
	}

	m := manipulation.GetGlobalManipulator()
	tx := newTestTx(t, wallets[0], ethAddresses[0], big.NewInt(1000))

	if m.ShouldCensor(tx) {
		t.Errorf("expected censorship to be disabled")
	}
	if m.ShouldPrioritize(tx) {
		t.Errorf("expected prioritization to be disabled")
	}
	if m.ShouldDropAsInjection(tx) {
		t.Errorf("expected injection detection to be disabled")
	}
}

func setupTestWallets(t *testing.T) ([]*secp256k1.PrivateKey, []common.Address) {
	wallets := make([]*secp256k1.PrivateKey, 5)
	ethAddresses := make([]common.Address, 5)
	for i := 0; i < 5; i++ {
		key, err := secp256k1.NewPrivateKey()
		if err != nil {
			t.Fatalf("failed to generate private key: %v", err)
		}
		wallets[i] = key
		ethAddresses[i] = crypto.PubkeyToAddress(*key.PublicKey().ToECDSA())
	}
	return wallets, ethAddresses
}

func newTestTx(t *testing.T, key *secp256k1.PrivateKey, to common.Address, amount *big.Int) *types.Transaction {
	chainID := big.NewInt(43112)
	nonce := uint64(0)
	gasLimit := uint64(21000)
	gasPrice := big.NewInt(1000000000) // 1 Gwei

	tx := types.NewTransaction(nonce, to, amount, gasLimit, gasPrice, nil)
	if key == nil {
		return tx
	}

	ethKey := key.ToECDSA()
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), ethKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	return signedTx
}
