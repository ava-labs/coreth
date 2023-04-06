// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package evm

import (
	"errors"
	"math/big"
	"strings"

	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = interfaces.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

// MultisigDataOwners is an auto generated low-level Go binding around an user-defined struct.
type MultisigDataOwners struct {
	Threshold *big.Int
	CtrlGroup []common.Address
}

// EvmMetaData contains all meta data concerning the Evm contract.
var EvmMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"aliases\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"threshold\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"key\",\"type\":\"address\"}],\"name\":\"getAlias\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"threshold\",\"type\":\"uint256\"},{\"internalType\":\"address[]\",\"name\":\"ctrlGroup\",\"type\":\"address[]\"}],\"internalType\":\"structMultisigData.Owners\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// EvmABI is the input ABI used to generate the binding from.
// Deprecated: Use EvmMetaData.ABI instead.
var EvmABI = EvmMetaData.ABI

// Evm is an auto generated Go binding around an Ethereum contract.
type Evm struct {
	EvmCaller     // Read-only binding to the contract
	EvmTransactor // Write-only binding to the contract
	EvmFilterer   // Log filterer for contract events
}

// EvmCaller is an auto generated read-only Go binding around an Ethereum contract.
type EvmCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EvmTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EvmTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EvmFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EvmFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EvmSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EvmSession struct {
	Contract     *Evm              // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EvmCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EvmCallerSession struct {
	Contract *EvmCaller    // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts // Call options to use throughout this session
}

// EvmTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EvmTransactorSession struct {
	Contract     *EvmTransactor    // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EvmRaw is an auto generated low-level Go binding around an Ethereum contract.
type EvmRaw struct {
	Contract *Evm // Generic contract binding to access the raw methods on
}

// EvmCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EvmCallerRaw struct {
	Contract *EvmCaller // Generic read-only contract binding to access the raw methods on
}

// EvmTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EvmTransactorRaw struct {
	Contract *EvmTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEvm creates a new instance of Evm, bound to a specific deployed contract.
func NewEvm(address common.Address, backend bind.ContractBackend) (*Evm, error) {
	contract, err := bindEvm(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Evm{EvmCaller: EvmCaller{contract: contract}, EvmTransactor: EvmTransactor{contract: contract}, EvmFilterer: EvmFilterer{contract: contract}}, nil
}

// NewEvmCaller creates a new read-only instance of Evm, bound to a specific deployed contract.
func NewEvmCaller(address common.Address, caller bind.ContractCaller) (*EvmCaller, error) {
	contract, err := bindEvm(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EvmCaller{contract: contract}, nil
}

// NewEvmTransactor creates a new write-only instance of Evm, bound to a specific deployed contract.
func NewEvmTransactor(address common.Address, transactor bind.ContractTransactor) (*EvmTransactor, error) {
	contract, err := bindEvm(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EvmTransactor{contract: contract}, nil
}

// NewEvmFilterer creates a new log filterer instance of Evm, bound to a specific deployed contract.
func NewEvmFilterer(address common.Address, filterer bind.ContractFilterer) (*EvmFilterer, error) {
	contract, err := bindEvm(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EvmFilterer{contract: contract}, nil
}

// bindEvm binds a generic wrapper to an already deployed contract.
func bindEvm(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(EvmABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Evm *EvmRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Evm.Contract.EvmCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Evm *EvmRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Evm.Contract.EvmTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Evm *EvmRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Evm.Contract.EvmTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Evm *EvmCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Evm.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Evm *EvmTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Evm.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Evm *EvmTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Evm.Contract.contract.Transact(opts, method, params...)
}

// Aliases is a free data retrieval call binding the contract method 0xc3bf2ba4.
//
// Solidity: function aliases(address ) view returns(uint256 threshold)
func (_Evm *EvmCaller) Aliases(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Evm.contract.Call(opts, &out, "aliases", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Aliases is a free data retrieval call binding the contract method 0xc3bf2ba4.
//
// Solidity: function aliases(address ) view returns(uint256 threshold)
func (_Evm *EvmSession) Aliases(arg0 common.Address) (*big.Int, error) {
	return _Evm.Contract.Aliases(&_Evm.CallOpts, arg0)
}

// Aliases is a free data retrieval call binding the contract method 0xc3bf2ba4.
//
// Solidity: function aliases(address ) view returns(uint256 threshold)
func (_Evm *EvmCallerSession) Aliases(arg0 common.Address) (*big.Int, error) {
	return _Evm.Contract.Aliases(&_Evm.CallOpts, arg0)
}

// GetAlias is a free data retrieval call binding the contract method 0x99900d11.
//
// Solidity: function getAlias(address key) view returns((uint256,address[]))
func (_Evm *EvmCaller) GetAlias(opts *bind.CallOpts, key common.Address) (MultisigDataOwners, error) {
	var out []interface{}
	err := _Evm.contract.Call(opts, &out, "getAlias", key)

	if err != nil {
		return *new(MultisigDataOwners), err
	}

	out0 := *abi.ConvertType(out[0], new(MultisigDataOwners)).(*MultisigDataOwners)

	return out0, err

}

// GetAlias is a free data retrieval call binding the contract method 0x99900d11.
//
// Solidity: function getAlias(address key) view returns((uint256,address[]))
func (_Evm *EvmSession) GetAlias(key common.Address) (MultisigDataOwners, error) {
	return _Evm.Contract.GetAlias(&_Evm.CallOpts, key)
}

// GetAlias is a free data retrieval call binding the contract method 0x99900d11.
//
// Solidity: function getAlias(address key) view returns((uint256,address[]))
func (_Evm *EvmCallerSession) GetAlias(key common.Address) (MultisigDataOwners, error) {
	return _Evm.Contract.GetAlias(&_Evm.CallOpts, key)
}
