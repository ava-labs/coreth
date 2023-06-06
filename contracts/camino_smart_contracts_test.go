// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package contracts

import (
	"context"
	"crypto/rand"
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/interfaces"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/accounts/abi/bind/backends"
	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethadmin"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/vmerrs"

	admin "github.com/ava-labs/coreth/contracts/build_contracts/admin/src"
)

var (
	ctx = context.Background()

	gasLimit = uint64(1)

	errAccessDeniedMsg = "execution reverted: Access denied"
	errUnkownRoleMsg   = "execution reverted: Unknown Role"
	errNotContractMsg  = "execution reverted: Not a contract"
	errRevokeAdminRole = "execution reverted: Cannot revoke ADMIN_ROLE from yourself"

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
	backend     *eth.Ethereum
	chainConfig *params.ChainConfig
}

// Src code of factory contract:

// // SPDX-License-Identifier: MIT
// pragma solidity ^0.8.0;
//
//	contract Factory {
//	    address public deployedContract;
//
//	    // Event emitted when a new contract is deployed
//	    event ContractDeployed(
//	        address indexed newContract,
//	        address indexed creator
//	    );
//
//	    // Deploy a new instance of the Contract contract
//	    function deployNestedContract() external {
//	        // Create a new instance of the Contract contract
//	        Contract newContract = new Contract();
//
//	        // Emit an event with the address of the newly deployed contract and the creator's address
//	        emit ContractDeployed(address(newContract), msg.sender);
//
//	        deployedContract = address(newContract);
//	    }
//
//	    function deployNestedContract2() external {
//	        Factory(address(this)).deployNestedContract();
//	    }
//	}
//
//	contract Contract {
//	    // Contract logic and state variables go here
//	    function alive() external pure returns (bool) {
//	        return true;
//	    }
//	}
func TestDeployContract(t *testing.T) {
	const dummyContractAbi = "[{\"inputs\":[],\"name\":\"Assert\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"OOG\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"PureRevert\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"Revert\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"Valid\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"
	const dummyContractBin = "0x60806040523480156100115760006000fd5b50610017565b61016e806100266000396000f3fe60806040523480156100115760006000fd5b506004361061005c5760003560e01c806350f6fe3414610062578063aa8b1d301461006c578063b9b046f914610076578063d8b9839114610080578063e09fface1461008a5761005c565b60006000fd5b61006a610094565b005b6100746100ad565b005b61007e6100b5565b005b6100886100c2565b005b610092610135565b005b6000600090505b5b808060010191505061009b565b505b565b60006000fd5b565b600015156100bf57fe5b5b565b6040517f08c379a000000000000000000000000000000000000000000000000000000000815260040180806020018281038252600d8152602001807f72657665727420726561736f6e0000000000000000000000000000000000000081526020015060200191505060405180910390fd5b565b5b56fea2646970667358221220345bbcbb1a5ecf22b53a78eaebf95f8ee0eceff6d10d4b9643495084d2ec934a64736f6c63430006040033"
	const factoryContractAbi = "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newContract\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"}],\"name\":\"ContractDeployed\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"deployNestedContract\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"deployNestedContract2\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"deployedContract\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]"
	const factoryContractBin = "0x608060405234801561001057600080fd5b5061033d806100206000396000f3fe608060405234801561001057600080fd5b50600436106100415760003560e01c80632ea7d78b146100465780635532a84514610050578063ae242cdf1461005a575b600080fd5b61004e610078565b005b610058610142565b005b6100626101a4565b60405161006f9190610215565b60405180910390f35b6000604051610086906101c8565b604051809103906000f0801580156100a2573d6000803e3d6000fd5b5090503373ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f33c981baba081f8fd2c52ac6ad1ea95b6814b4376640f55689051f658472968860405160405180910390a3806000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050565b3073ffffffffffffffffffffffffffffffffffffffff16632ea7d78b6040518163ffffffff1660e01b8152600401600060405180830381600087803b15801561018a57600080fd5b505af115801561019e573d6000803e3d6000fd5b50505050565b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60d78061023183390190565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006101ff826101d4565b9050919050565b61020f816101f4565b82525050565b600060208201905061022a6000830184610206565b9291505056fe608060405234801561001057600080fd5b5060b88061001f6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c8063753899e914602d575b600080fd5b60336047565b604051603e91906069565b60405180910390f35b60006001905090565b60008115159050919050565b6063816050565b82525050565b6000602082019050607c6000830184605c565b9291505056fea2646970667358221220aa7774fbe77b64cf0fc6b79f9ac78d855c8c9356964910b05cc7674e38f4a17064736f6c63430008120033a2646970667358221220a9e0d9f11e865d738ecf0beb10f1323de578ac89dc9ca1a2d732a96baf335b0c64736f6c63430008120033"
	const nestedContractAbi = "[{\"inputs\": [],\"name\": \"alive\",\"outputs\": [{\"internalType\": \"bool\",\"name\": \"\",\"type\": \"bool\"}],\"stateMutability\": \"pure\",\"type\": \"function\"}]"

	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for admin
	adminOpts, err := bind.NewKeyedTransactorWithChainID(adminKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Initialize TransactOpts for kycAddress
	kycAddrOpts, err := bind.NewKeyedTransactorWithChainID(kycKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	ethChain := newETHChain(t)
	ac := ethadmin.NewController(ethChain.backend.APIBackend, ethChain.chainConfig)

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdminAndAdminController(alloc, gasLimit, adminAddr, ac)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

	adminContract, err := admin.NewBuild(contractAddr, sim)
	assert.NoError(t, err)

	// BuildSession Initialization for admin
	adminSession := admin.BuildSession{Contract: adminContract, TransactOpts: *adminOpts}

	// BuildSession Initialization for kycAddr
	kycAddrSession := admin.BuildSession{Contract: adminContract, TransactOpts: *kycAddrOpts}

	// Grant role to kycAddr
	_, err = adminSession.GrantRole(kycAddr, KYC_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	// Check if the role is granted
	kycRole, err := adminSession.HasRole(kycAddr, KYC_ROLE)
	assert.NoError(t, err)
	assert.True(t, kycRole)

	// Add kyc state to kycAddr
	_, err = kycAddrSession.ApplyKycState(kycAddr, false, big.NewInt(1))
	assert.NoError(t, err)
	sim.Commit(true)

	// Deploy contract with kycAddr, we assume it will pass
	parsed, _ := abi.JSON(strings.NewReader(dummyContractAbi))
	_, _, _, err = bind.DeployContract(kycAddrOpts, parsed, common.FromHex(dummyContractBin), sim)
	sim.Commit(true)
	assert.NoError(t, err)

	// Deploy factory contract with nested contract, we assume it will pass
	parsedABI, _ := abi.JSON(strings.NewReader(factoryContractAbi))
	factoryAddr, _, boundContract, err := bind.DeployContract(kycAddrOpts, parsedABI, common.FromHex(factoryContractBin), sim)
	sim.Commit(true)
	assert.NoError(t, err)

	_, err = boundContract.Transact(&bind.TransactOpts{From: kycAddr, Signer: kycAddrOpts.Signer}, "deployNestedContract")
	assert.NoError(t, err)
	sim.Commit(true)

	ctx := context.Background()
	// Get the method ID of the 'nested' function
	methodID := parsedABI.Methods["deployedContract"].ID
	res, err := sim.AcceptedCallContract(ctx, interfaces.CallMsg{
		From: kycAddr,
		To:   &factoryAddr,
		Data: methodID,
	})
	assert.NoError(t, err)
	assert.True(t, len(res) > 0, "result of contract call was empty")

	nestedContract := common.BytesToAddress(res)

	parsedNestedContractABI, _ := abi.JSON(strings.NewReader(nestedContractAbi))
	methodID = parsedNestedContractABI.Methods["alive"].ID

	alive, err := sim.AcceptedCallContract(ctx, interfaces.CallMsg{
		From: kycAddr,
		To:   &nestedContract,
		Data: methodID,
	})
	assert.NoError(t, err)
	assert.True(t, len(alive) > 0)

	// Deploy the SC via external call
	_, err = boundContract.Transact(&bind.TransactOpts{From: kycAddr, Signer: kycAddrOpts.Signer}, "deployNestedContract2")
	assert.NoError(t, err)
	sim.Commit(true)

	// Get the method ID of the 'nested' function
	methodID = parsedABI.Methods["deployedContract"].ID
	res, err = sim.AcceptedCallContract(ctx, interfaces.CallMsg{
		From: kycAddr,
		To:   &factoryAddr,
		Data: methodID,
	})
	assert.NoError(t, err)
	assert.True(t, len(res) > 0, "result of contract call was empty")

	nestedContract = common.BytesToAddress(res)
	methodID = parsedNestedContractABI.Methods["alive"].ID

	alive, err = sim.AcceptedCallContract(ctx, interfaces.CallMsg{
		From: kycAddr,
		To:   &nestedContract,
		Data: methodID,
	})
	assert.NoError(t, err)
	assert.True(t, len(alive) > 0)

	// Remove kyc state to kycAddr
	_, err = kycAddrSession.ApplyKycState(kycAddr, true, big.NewInt(1))
	assert.NoError(t, err)

	sim.Commit(true)

	// Deploy contract again with kycAddr, we assume it will fail
	_, _, _, err = bind.DeployContract(kycAddrOpts, parsed, common.FromHex(dummyContractBin), sim)
	sim.Commit(true)
	assert.Error(t, err, vmerrs.ErrNotKycVerified)
}

func TestAdminRoleFunctions(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize FilterOpts for event filtering
	filterOpts := bind.FilterOpts{Context: ctx, Start: 1, End: nil}

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
	expectedRoles := addAndVerifyRoles(t, adminSession, sim)

	// Test Revoke Role
	// Delete blacklist role from blacklist address
	_, err = adminSession.RevokeRole(blacklistAddr, BLACKLIST_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	// Validate that there is no role in the address
	role, err := adminSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	assert.EqualValues(t, big.NewInt(0), big.NewInt(int64(common.Big0.Cmp(role))))

	// Delete admin role from admin address
	_, err = adminSession.RevokeRole(adminAddr, ADMIN_ROLE)
	assert.EqualError(t, err, errRevokeAdminRole)

	sim.Commit(true)

	adminRole, err = adminSession.HasRole(adminAddr, ADMIN_ROLE)
	assert.NoError(t, err)

	// Assertion to check if Initial Admin address has indeed the admin role
	assert.True(t, adminRole)

	// Add the blacklist role again
	_, err = adminSession.GrantRole(blacklistAddr, BLACKLIST_ROLE)
	assert.NoError(t, err)

	sim.Commit(true)

	role, err = adminSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	assert.EqualValues(t, BLACKLIST_ROLE, role)
	expectedRoles = append(expectedRoles, role)

	// Test Upgrade function
	// Initially, with an non-contract address. Should fail.
	_, err = adminSession.Upgrade(adminAddr)
	assert.EqualError(t, err, errNotContractMsg)

	// Then, with a contract address
	_, err = adminSession.Upgrade(contractAddr)
	assert.NoError(t, err)

	// Event Testing
	dropCounter := 0 // Counter of the total drop events
	dropItr, err := adminContract.FilterDropRole(&filterOpts)
	assert.NoError(t, err)
	assert.NotNil(t, dropItr)

	for dropItr.Next() {
		dropCounter++
		event := dropItr.Event
		assert.NotNil(t, event)
		assert.EqualValues(t, event.Role, BLACKLIST_ROLE)
	}
	// Only one drop event has been emitted (Revoke Role) therefore, counter's value should be 1
	assert.EqualValues(t, 1, dropCounter)

	setCounter := 0 // Counter of the total set role events
	setItr, err := adminContract.FilterSetRole(&filterOpts)
	assert.NoError(t, err)
	assert.NotNil(t, setItr)

	for setItr.Next() {
		event := setItr.Event
		assert.NotNil(t, event)
		assert.EqualValues(t, event.Role, expectedRoles[setCounter])
		setCounter++
	}
	// Four Set Roles events have been emitted in total
	// (including the events in addAndVerifyRoles function) therefore, counter's value should be 4
	assert.EqualValues(t, 4, setCounter)
}

func TestKycRoleFunctions(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize FilterOpts for event filtering
	filterOpts := bind.FilterOpts{Context: ctx, Start: 1, End: nil}

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

	// Event Testing
	setCounter := 0 // Counter of the total set role events
	setItr, err := adminContract.FilterSetRole(&filterOpts)
	assert.NoError(t, err)
	assert.NotNil(t, setItr)

	for setItr.Next() {
		setCounter++
		event := setItr.Event
		assert.NotNil(t, event)
		assert.EqualValues(t, event.Role, KYC_ROLE)
	}
	// Only one set role event has been emitted therefore, counter's value should be 1
	assert.EqualValues(t, 1, setCounter)

	kycCounter := 0 // Counter of the total set role events
	kycAddresses := make([]common.Address, 1)
	kycAddresses = append(kycAddresses, kycAddr)
	kycItr, err := adminContract.FilterKycStateChanged(&filterOpts, kycAddresses)
	assert.NoError(t, err)
	assert.NotNil(t, kycItr)

	for kycItr.Next() {
		kycCounter++
		event := kycItr.Event
		assert.NotNil(t, event)
		assert.EqualValues(t, event.NewState, big.NewInt(1))
	}
	// Only one KYC State Changed event has been emitted therefore, counter's value should be 1
	assert.EqualValues(t, 1, kycCounter)
}

func TestGasFeeFunctions(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize FilterOpts for event filtering
	filterOpts := bind.FilterOpts{Context: ctx, Start: 1, End: nil}

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

	// Event Testing
	setCounter := 0 // Counter of the total set role events
	setItr, err := adminContract.FilterSetRole(&filterOpts)
	assert.NoError(t, err)
	assert.NotNil(t, setItr)

	for setItr.Next() {
		setCounter++
		event := setItr.Event
		assert.NotNil(t, event)
		assert.EqualValues(t, event.Role, GAS_FEE_ROLE)
	}
	// Only one set role event has been emitted therefore, counter's value should be 1
	assert.EqualValues(t, 1, setCounter)

	gasCounter := 0 // Counter of the total set role events
	gasItr, err := adminContract.FilterGasFeeSet(&filterOpts)
	assert.NoError(t, err)
	assert.NotNil(t, gasItr)

	for gasItr.Next() {
		gasCounter++
		event := gasItr.Event
		assert.NotNil(t, event)
		assert.EqualValues(t, event.NewGasFee, big.NewInt(1))
	}
	// Only one KYC State Changed event has been emitted therefore, counter's value should be 1
	assert.EqualValues(t, 1, gasCounter)
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

	// Initialize FilterOpts for event filtering
	filterOpts := bind.FilterOpts{Context: ctx, Start: 1, End: nil}

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

	// Event Testing
	// No events have been emitted so normally, nothing should be filtered
	setItr, err := adminContract.FilterSetRole(&filterOpts)
	assert.NoError(t, err)
	assert.False(t, setItr.Next())

	dropItr, err := adminContract.FilterDropRole(&filterOpts)
	assert.NoError(t, err)
	assert.False(t, dropItr.Next())

	gasItr, err := adminContract.FilterGasFeeSet(&filterOpts)
	assert.NoError(t, err)
	assert.False(t, gasItr.Next())

	kycAddresses := make([]common.Address, 1)
	kycAddresses = append(kycAddresses, kycAddr)
	kycItr, err := adminContract.FilterKycStateChanged(&filterOpts, kycAddresses)
	assert.NoError(t, err)
	assert.False(t, kycItr.Next())
}

func TestEthAdmin(t *testing.T) {
	contractAddr := AdminProxyAddr

	// Initialize TransactOpts for each key
	gasFeeOpts, err := bind.NewKeyedTransactorWithChainID(gasFeeKey, big.NewInt(1337))
	assert.NoError(t, err)

	// Generate GenesisAlloc
	alloc := makeGenesisAllocation()

	// Create a bew Eth chain to generate an AdminController from its backend
	// Simulated backed will not do
	ethChain := newETHChain(t)
	ac := ethadmin.NewController(ethChain.backend.APIBackend, ethChain.chainConfig)

	// Generate SimulatedBackend
	sim := backends.NewSimulatedBackendWithInitialAdminAndAdminController(alloc, gasLimit, gasFeeAddr, ac)
	defer func() {
		err := sim.Close()
		assert.NoError(t, err)
	}()

	sim.Commit(true)

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
		ChainID:                         chainID,
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    big.NewInt(0),
		DAOForkSupport:                  true,
		EIP150Block:                     big.NewInt(0),
		EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		SunrisePhase0BlockTimestamp:     big.NewInt(0),
		ApricotPhase1BlockTimestamp:     big.NewInt(0),
		ApricotPhase2BlockTimestamp:     big.NewInt(0),
		ApricotPhase3BlockTimestamp:     big.NewInt(0),
		ApricotPhase4BlockTimestamp:     big.NewInt(0),
		ApricotPhase5BlockTimestamp:     big.NewInt(0),
		ApricotPhasePre6BlockTimestamp:  big.NewInt(0),
		ApricotPhase6BlockTimestamp:     big.NewInt(0),
		ApricotPhasePost6BlockTimestamp: big.NewInt(0),
		BanffBlockTimestamp:             big.NewInt(0),
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

	chain := &ETHChain{backend: backend, chainConfig: chainConfig}
	backend.SetEtherbase(blackholeAddr)

	return chain
}

func makeGenesisAllocation() core.GenesisAlloc {
	alloc := make(core.GenesisAlloc)

	eth100 := big.NewInt(100)
	eth100.Mul(eth100, big.NewInt(params.Ether))

	alloc[adminAddr] = core.GenesisAccount{Balance: eth100}
	alloc[kycAddr] = core.GenesisAccount{Balance: eth100}
	alloc[gasFeeAddr] = core.GenesisAccount{Balance: eth100}
	alloc[blacklistAddr] = core.GenesisAccount{Balance: eth100}
	alloc[dummyAddr] = core.GenesisAccount{Balance: eth100}

	return alloc
}

func addAndVerifyRoles(t *testing.T, adminSession admin.BuildSession, sim *backends.SimulatedBackend) []*big.Int {
	roles := make([]*big.Int, 0)

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

	kycRole, err := adminSession.GetRoles(kycAddr)
	assert.NoError(t, err)
	assert.Equal(t, KYC_ROLE, kycRole)
	roles = append(roles, kycRole)

	gasFeeRole, err := adminSession.GetRoles(gasFeeAddr)
	assert.NoError(t, err)
	assert.Equal(t, GAS_FEE_ROLE, gasFeeRole)
	roles = append(roles, gasFeeRole)

	blacklistRole, err := adminSession.GetRoles(blacklistAddr)
	assert.NoError(t, err)
	assert.Equal(t, BLACKLIST_ROLE, blacklistRole)
	roles = append(roles, blacklistRole)

	return roles
}
