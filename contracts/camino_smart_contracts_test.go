// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package contracts

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/accounts/abi/bind/backends"
	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/consensus/dummy"
	admin "github.com/ava-labs/coreth/contracts/build_contracts/admin/src"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethadmin"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/node"

	"github.com/ava-labs/coreth/params"
)

var (
	gasLimit = uint64(1)

	errAccessDeniedMsg = "execution reverted: Access denied"
	errUnkownRoleMsg   = "execution reverted: Unknown Role"
	errNotContractMsg  = "execution reverted: Not a contract"

	adminKey, _     = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	kycKey, _       = crypto.HexToECDSA("0e6de2b744bd97ab6abd2e8fc624befbac7ed5b37e8a7e8ddd164e23d7ac06be")
	gasFeeKey, _    = crypto.HexToECDSA("04214cc61e1feaf005aa25b7771d33ca5c4aea959d21fe9a1429f822fa024171")
	blacklistKey, _ = crypto.HexToECDSA("b32d5aa5b8f4028218538c8c5b14b5c14f3f2e35b236e4bbbff09b669e69e46c")
	dummyKey, _     = crypto.HexToECDSA("62802c57c0e3c24ae0ce354f7d19f7659ddbe506547b00e9e6a722980d2fed3d")
	blackHoleKey, _ = crypto.HexToECDSA("50cdff9c21002158e2a18b73c2504f8982332e05b5c3b26d3cffdd2f1291796a")

	adminAddr     = crypto.PubkeyToAddress(adminKey.PublicKey)
	kycAddr       = crypto.PubkeyToAddress(kycKey.PublicKey)
	gasFeeAddr    = crypto.PubkeyToAddress(gasFeeKey.PublicKey)
	blacklistAddr = crypto.PubkeyToAddress(blacklistKey.PublicKey)
	dummyAddr     = crypto.PubkeyToAddress(dummyKey.PublicKey)
	blackholeAddr = crypto.PubkeyToAddress(blackHoleKey.PublicKey)

	AdminProxyAddr = common.HexToAddress("0x010000000000000000000000000000000000000a")

	ADMIN_ROLE     = big.NewInt(1)
	GAS_FEE_ROLE   = big.NewInt(2)
	KYC_ROLE       = big.NewInt(4)
	BLACKLIST_ROLE = big.NewInt(8)
)

type ETHChain struct {
	backend *eth.Ethereum
}

func TestAdminRoleFunctions(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	adminOpts, err := bind.NewKeyedTransactorWithChainID(adminKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdmin(alloc, gasLimit, adminAddr)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization
	adminSession := admin.BuildSession{Contract: adminContract, TransactOpts: *adminOpts}

	adminRole, err := adminSession.HasRole(adminAddr, ADMIN_ROLE)
	assert.NoError(t, err)

	// Assertion to check if Initial Admin address has indeed the admin role
	assert.True(t, adminRole)

	// Initialize role in every address (test GrantRole function)
	addAndVerifyRoles(t, adminSession, sim)

	// Test Revoke Role
	// Delete blacklist role from blacklist address
	_, err = adminSession.RevokeRole(blacklistAddr, BLACKLIST_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	// Validate that there is no role in the address
	role, err := adminSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(common.Big0.Cmp(role))))

	// Add the blacklist role again
	_, err = adminSession.GrantRole(blacklistAddr, BLACKLIST_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	role, err = adminSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	assert.EqualValues(t, BLACKLIST_ROLE, role)

	// Test Upgrade function
	// Initially, with an non-contract address. Should fail.
	_, err = adminSession.Upgrade(adminAddr)
	assert.EqualError(t, err, errNotContractMsg)

	// Lastly, with a contract address
	_, err = adminSession.Upgrade(contractAddr)
	assert.NoError(t, err)
}

func TestKycRoleFunctions(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	kycOpts, err := bind.NewKeyedTransactorWithChainID(kycKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdmin(alloc, gasLimit, kycAddr)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization
	kycSession := admin.BuildSession{Contract: adminContract, TransactOpts: *kycOpts}

	// Add KYC role in the address
	_, err = kycSession.GrantRole(kycAddr, KYC_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	role, err := kycSession.GetRoles(kycAddr)
	assert.NoError(t, err)
	// At this point, kycAddr has both Admin and KYC Role
	assert.Equal(t, big.NewInt(5), role)

	st, err := kycSession.GetKycState(kycAddr)
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(common.Big0.Cmp(st))))

	_, err = kycSession.ApplyKycState(kycAddr, false, big.NewInt(1))
	assert.NoError(t, err)

	sim.Commit(true)

	st, err = kycSession.GetKycState(kycAddr)
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(1), st)
}

func TestGasFeeFunctions(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	gasFeeOpts, err := bind.NewKeyedTransactorWithChainID(gasFeeKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdmin(alloc, gasLimit, gasFeeAddr)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization
	gasFeeSession := admin.BuildSession{Contract: adminContract, TransactOpts: *gasFeeOpts}

	// Add Gas Fee Role in the address
	_, err = gasFeeSession.GrantRole(gasFeeAddr, GAS_FEE_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	role, err := gasFeeSession.GetRoles(gasFeeAddr)
	assert.NoError(t, err)
	// At this point, gasFeeAddr has both Admin and Gas Fee Role
	assert.Equal(t, big.NewInt(3), role)

	bf, err := gasFeeSession.GetBaseFee()
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(common.Big0.Cmp(bf))))

	_, err = gasFeeSession.SetBaseFee(big.NewInt(1))
	assert.NoError(t, err)

	sim.Commit(true)

	bf, err = gasFeeSession.GetBaseFee()
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(1), bf)
}

func TestBlacklistFunctions(t *testing.T) {
	var signature [4]byte
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	blacklistOpts, err := bind.NewKeyedTransactorWithChainID(blacklistKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdmin(alloc, gasLimit, blacklistAddr)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization
	blacklistSession := admin.BuildSession{Contract: adminContract, TransactOpts: *blacklistOpts}

	// Add Gas Fee Role in the address
	_, err = blacklistSession.GrantRole(blacklistAddr, BLACKLIST_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	role, err := blacklistSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	// At this point, blacklistAddr has both Admin and Blacklist Role
	assert.Equal(t, big.NewInt(9), role)

	blst, err := blacklistSession.GetBlacklistState(blacklistAddr, signature)
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(common.Big0.Cmp(blst))))

	_, err = blacklistSession.SetBlacklistState(blacklistAddr, signature, big.NewInt(1))
	assert.NoError(t, err)

	sim.Commit(true)

	blst, err = blacklistSession.GetBlacklistState(blacklistAddr, signature)
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(1), blst)
}

func TestDummySession(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	dummyOpts, err := bind.NewKeyedTransactorWithChainID(dummyKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdmin(alloc, gasLimit, adminAddr)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization
	dummySession := admin.BuildSession{Contract: adminContract, TransactOpts: *dummyOpts}

	// At this point, dummyAddr has no roles and therefore, should not be able to call any function
	_, err = dummySession.SetBaseFee(big.NewInt(1))
	assert.EqualError(t, err, errAccessDeniedMsg)

	sim.Commit(true)

	_, err = dummySession.ApplyKycState(dummyAddr, false, big.NewInt(1))
	assert.EqualError(t, err, errAccessDeniedMsg)

	sim.Commit(true)

	_, err = dummySession.GrantRole(dummyAddr, ADMIN_ROLE)
	assert.EqualError(t, err, errAccessDeniedMsg)
}

func TestEthAdmin(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	gasFeeOpts, err := bind.NewKeyedTransactorWithChainID(gasFeeKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdmin(alloc, gasLimit, gasFeeAddr)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	// Create a bew Eth chain to generate an AdminController from its backend
	// Simulated backed will not do
	ethChain := newETHChain(t)

	ac := ethadmin.NewController(ethChain.backend.APIBackend)
	sim.Blockchain().SetAdminController(ac)

	latestHeader, state := getLatestHeaderAndState(t, sim)

	bf := ac.GetFixedBaseFee(latestHeader, state)
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(bf.Cmp(big.NewInt(int64(params.SunrisePhase0BaseFee))))))

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization
	gasFeeSession := admin.BuildSession{Contract: adminContract, TransactOpts: *gasFeeOpts}

	// Add Gas Fee Role in the address
	_, err = gasFeeSession.GrantRole(gasFeeAddr, GAS_FEE_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	_, err = gasFeeSession.SetBaseFee(big.NewInt(1))
	assert.NoError(t, err)

	sim.Commit(true)

	bf = ac.GetFixedBaseFee(latestHeader, state)
	// Despite the fact that the base fee changed, the Admin Controller still tries to get it from the previous role.
	// Therefore, it should return the SunrisePhase0BaseFee value
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(bf.Cmp(big.NewInt(int64(params.SunrisePhase0BaseFee))))))

	// Get new block's header
	latestHeader, state = getLatestHeaderAndState(t, sim)

	bf = ac.GetFixedBaseFee(latestHeader, state)

	// Now with the new block's header, Base Fee should be the new one.
	assert.EqualValues(t, big.NewInt(1), bf)
}

func getLatestHeaderAndState(t *testing.T, sim *backends.SimulatedBackend) (*types.Header, *state.StateDB) {
	latestHeader := sim.Blockchain().LastAcceptedBlock().Header()
	state, err := sim.Blockchain().State()
	assert.NoError(t, err)

	return latestHeader, state
}

// newETHChain creates an Ethereum blockchain with the given configs.
func newETHChain(t *testing.T) *ETHChain {
	chainID := big.NewInt(1)
	initialBalance := big.NewInt(1000000000000000000)

	fundedKey, err := keystore.NewKey(rand.Reader)
	assert.NoError(t, err)

	// configure the chain
	config := ethconfig.NewDefaultConfig()
	chainConfig := &params.ChainConfig{
		ChainID:             chainID,
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        big.NewInt(0),
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
	}

	config.Genesis = &core.Genesis{
		Config:     chainConfig,
		Nonce:      0,
		Number:     0,
		ExtraData:  hexutil.MustDecode("0x00"),
		GasLimit:   gasLimit,
		Difficulty: big.NewInt(0),
		Alloc:      core.GenesisAlloc{fundedKey.Address: {Balance: initialBalance}},
	}

	node, err := node.New(&node.Config{})
	assert.NoError(t, err)

	backend, err := eth.New(node, &config, new(dummy.ConsensusCallbacks), rawdb.NewMemoryDatabase(), eth.DefaultSettings, common.Hash{}, &mockable.Clock{})
	assert.NoError(t, err)

	chain := &ETHChain{backend: backend}
	backend.SetEtherbase(blackholeAddr)

	return chain
}

func makeGenesisAllocation() core.GenesisAlloc {
	alloc := make(core.GenesisAlloc)

	alloc[adminAddr] = core.GenesisAccount{Balance: big.NewInt(params.Ether)}
	alloc[kycAddr] = core.GenesisAccount{Balance: big.NewInt(params.Ether)}
	alloc[gasFeeAddr] = core.GenesisAccount{Balance: big.NewInt(params.Ether)}
	alloc[blacklistAddr] = core.GenesisAccount{Balance: big.NewInt(params.Ether)}
	alloc[dummyAddr] = core.GenesisAccount{Balance: big.NewInt(params.Ether)}

	return alloc
}

func addAndVerifyRoles(t *testing.T, adminSession admin.BuildSession, sim *backends.SimulatedBackend) {
	// Add and verify Roles
	_, err := adminSession.GrantRole(kycAddr, KYC_ROLE)
	assert.NoError(t, err)

	_, err = adminSession.GrantRole(gasFeeAddr, GAS_FEE_ROLE)
	assert.NoError(t, err)

	_, err = adminSession.GrantRole(blacklistAddr, BLACKLIST_ROLE)
	assert.NoError(t, err)

	_, err = adminSession.GrantRole(dummyAddr, big.NewInt(100))
	assert.EqualError(t, err, errUnkownRoleMsg)

	sim.Commit(true)

	gasFeeRole, err := adminSession.GetRoles(gasFeeAddr)
	assert.NoError(t, err)
	assert.Equal(t, GAS_FEE_ROLE, gasFeeRole)

	kycRole, err := adminSession.GetRoles(kycAddr)
	assert.NoError(t, err)
	assert.Equal(t, KYC_ROLE, kycRole)

	blacklistRole, err := adminSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	assert.Equal(t, BLACKLIST_ROLE, blacklistRole)
}
