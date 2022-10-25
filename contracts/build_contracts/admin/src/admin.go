// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package build

import (
	"errors"
	"math/big"
	"strings"

	"github.com/chain4travel/caminoethvm/accounts/abi"
	"github.com/chain4travel/caminoethvm/accounts/abi/bind"
	"github.com/chain4travel/caminoethvm/core/types"
	"github.com/chain4travel/caminoethvm/interfaces"
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

// BuildMetaData contains all meta data concerning the Build contract.
var BuildMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"name\":\"DropRole\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newGasFee\",\"type\":\"uint256\"}],\"name\":\"GasFeeSet\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldState\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newState\",\"type\":\"uint256\"}],\"name\":\"KycStateChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"name\":\"SetRole\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bool\",\"name\":\"remove\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"state\",\"type\":\"uint256\"}],\"name\":\"applyKycState\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getBaseFee\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes4\",\"name\":\"signature\",\"type\":\"bytes4\"}],\"name\":\"getBlacklistState\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"getKycState\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"getRoles\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newFee\",\"type\":\"uint256\"}],\"name\":\"setBaseFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes4\",\"name\":\"signature\",\"type\":\"bytes4\"},{\"internalType\":\"uint256\",\"name\":\"state\",\"type\":\"uint256\"}],\"name\":\"setBlacklistState\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"}],\"name\":\"upgrade\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// BuildABI is the input ABI used to generate the binding from.
// Deprecated: Use BuildMetaData.ABI instead.
var BuildABI = BuildMetaData.ABI

// Build is an auto generated Go binding around an Ethereum contract.
type Build struct {
	BuildCaller     // Read-only binding to the contract
	BuildTransactor // Write-only binding to the contract
	BuildFilterer   // Log filterer for contract events
}

// BuildCaller is an auto generated read-only Go binding around an Ethereum contract.
type BuildCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// BuildTransactor is an auto generated write-only Go binding around an Ethereum contract.
type BuildTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// BuildFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type BuildFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// BuildSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type BuildSession struct {
	Contract     *Build            // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// BuildCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type BuildCallerSession struct {
	Contract *BuildCaller  // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts // Call options to use throughout this session
}

// BuildTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type BuildTransactorSession struct {
	Contract     *BuildTransactor  // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// BuildRaw is an auto generated low-level Go binding around an Ethereum contract.
type BuildRaw struct {
	Contract *Build // Generic contract binding to access the raw methods on
}

// BuildCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type BuildCallerRaw struct {
	Contract *BuildCaller // Generic read-only contract binding to access the raw methods on
}

// BuildTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type BuildTransactorRaw struct {
	Contract *BuildTransactor // Generic write-only contract binding to access the raw methods on
}

// NewBuild creates a new instance of Build, bound to a specific deployed contract.
func NewBuild(address common.Address, backend bind.ContractBackend) (*Build, error) {
	contract, err := bindBuild(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Build{BuildCaller: BuildCaller{contract: contract}, BuildTransactor: BuildTransactor{contract: contract}, BuildFilterer: BuildFilterer{contract: contract}}, nil
}

// NewBuildCaller creates a new read-only instance of Build, bound to a specific deployed contract.
func NewBuildCaller(address common.Address, caller bind.ContractCaller) (*BuildCaller, error) {
	contract, err := bindBuild(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &BuildCaller{contract: contract}, nil
}

// NewBuildTransactor creates a new write-only instance of Build, bound to a specific deployed contract.
func NewBuildTransactor(address common.Address, transactor bind.ContractTransactor) (*BuildTransactor, error) {
	contract, err := bindBuild(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &BuildTransactor{contract: contract}, nil
}

// NewBuildFilterer creates a new log filterer instance of Build, bound to a specific deployed contract.
func NewBuildFilterer(address common.Address, filterer bind.ContractFilterer) (*BuildFilterer, error) {
	contract, err := bindBuild(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &BuildFilterer{contract: contract}, nil
}

// bindBuild binds a generic wrapper to an already deployed contract.
func bindBuild(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(BuildABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Build *BuildRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Build.Contract.BuildCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Build *BuildRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Build.Contract.BuildTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Build *BuildRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Build.Contract.BuildTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Build *BuildCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Build.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Build *BuildTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Build.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Build *BuildTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Build.Contract.contract.Transact(opts, method, params...)
}

// GetBaseFee is a free data retrieval call binding the contract method 0x15e812ad.
//
// Solidity: function getBaseFee() view returns(uint256)
func (_Build *BuildCaller) GetBaseFee(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Build.contract.Call(opts, &out, "getBaseFee")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetBaseFee is a free data retrieval call binding the contract method 0x15e812ad.
//
// Solidity: function getBaseFee() view returns(uint256)
func (_Build *BuildSession) GetBaseFee() (*big.Int, error) {
	return _Build.Contract.GetBaseFee(&_Build.CallOpts)
}

// GetBaseFee is a free data retrieval call binding the contract method 0x15e812ad.
//
// Solidity: function getBaseFee() view returns(uint256)
func (_Build *BuildCallerSession) GetBaseFee() (*big.Int, error) {
	return _Build.Contract.GetBaseFee(&_Build.CallOpts)
}

// GetBlacklistState is a free data retrieval call binding the contract method 0x4de525d9.
//
// Solidity: function getBlacklistState(address account, bytes4 signature) view returns(uint256)
func (_Build *BuildCaller) GetBlacklistState(opts *bind.CallOpts, account common.Address, signature [4]byte) (*big.Int, error) {
	var out []interface{}
	err := _Build.contract.Call(opts, &out, "getBlacklistState", account, signature)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetBlacklistState is a free data retrieval call binding the contract method 0x4de525d9.
//
// Solidity: function getBlacklistState(address account, bytes4 signature) view returns(uint256)
func (_Build *BuildSession) GetBlacklistState(account common.Address, signature [4]byte) (*big.Int, error) {
	return _Build.Contract.GetBlacklistState(&_Build.CallOpts, account, signature)
}

// GetBlacklistState is a free data retrieval call binding the contract method 0x4de525d9.
//
// Solidity: function getBlacklistState(address account, bytes4 signature) view returns(uint256)
func (_Build *BuildCallerSession) GetBlacklistState(account common.Address, signature [4]byte) (*big.Int, error) {
	return _Build.Contract.GetBlacklistState(&_Build.CallOpts, account, signature)
}

// GetKycState is a free data retrieval call binding the contract method 0xbb03fa1d.
//
// Solidity: function getKycState(address account) view returns(uint256)
func (_Build *BuildCaller) GetKycState(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Build.contract.Call(opts, &out, "getKycState", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetKycState is a free data retrieval call binding the contract method 0xbb03fa1d.
//
// Solidity: function getKycState(address account) view returns(uint256)
func (_Build *BuildSession) GetKycState(account common.Address) (*big.Int, error) {
	return _Build.Contract.GetKycState(&_Build.CallOpts, account)
}

// GetKycState is a free data retrieval call binding the contract method 0xbb03fa1d.
//
// Solidity: function getKycState(address account) view returns(uint256)
func (_Build *BuildCallerSession) GetKycState(account common.Address) (*big.Int, error) {
	return _Build.Contract.GetKycState(&_Build.CallOpts, account)
}

// GetRoles is a free data retrieval call binding the contract method 0xce6ccfaf.
//
// Solidity: function getRoles(address addr) view returns(uint256)
func (_Build *BuildCaller) GetRoles(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Build.contract.Call(opts, &out, "getRoles", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRoles is a free data retrieval call binding the contract method 0xce6ccfaf.
//
// Solidity: function getRoles(address addr) view returns(uint256)
func (_Build *BuildSession) GetRoles(addr common.Address) (*big.Int, error) {
	return _Build.Contract.GetRoles(&_Build.CallOpts, addr)
}

// GetRoles is a free data retrieval call binding the contract method 0xce6ccfaf.
//
// Solidity: function getRoles(address addr) view returns(uint256)
func (_Build *BuildCallerSession) GetRoles(addr common.Address) (*big.Int, error) {
	return _Build.Contract.GetRoles(&_Build.CallOpts, addr)
}

// HasRole is a free data retrieval call binding the contract method 0x5c97f4a2.
//
// Solidity: function hasRole(address addr, uint256 role) view returns(bool)
func (_Build *BuildCaller) HasRole(opts *bind.CallOpts, addr common.Address, role *big.Int) (bool, error) {
	var out []interface{}
	err := _Build.contract.Call(opts, &out, "hasRole", addr, role)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x5c97f4a2.
//
// Solidity: function hasRole(address addr, uint256 role) view returns(bool)
func (_Build *BuildSession) HasRole(addr common.Address, role *big.Int) (bool, error) {
	return _Build.Contract.HasRole(&_Build.CallOpts, addr, role)
}

// HasRole is a free data retrieval call binding the contract method 0x5c97f4a2.
//
// Solidity: function hasRole(address addr, uint256 role) view returns(bool)
func (_Build *BuildCallerSession) HasRole(addr common.Address, role *big.Int) (bool, error) {
	return _Build.Contract.HasRole(&_Build.CallOpts, addr, role)
}

// ApplyKycState is a paid mutator transaction binding the contract method 0x9a11b2e8.
//
// Solidity: function applyKycState(address account, bool remove, uint256 state) returns()
func (_Build *BuildTransactor) ApplyKycState(opts *bind.TransactOpts, account common.Address, remove bool, state *big.Int) (*types.Transaction, error) {
	return _Build.contract.Transact(opts, "applyKycState", account, remove, state)
}

// ApplyKycState is a paid mutator transaction binding the contract method 0x9a11b2e8.
//
// Solidity: function applyKycState(address account, bool remove, uint256 state) returns()
func (_Build *BuildSession) ApplyKycState(account common.Address, remove bool, state *big.Int) (*types.Transaction, error) {
	return _Build.Contract.ApplyKycState(&_Build.TransactOpts, account, remove, state)
}

// ApplyKycState is a paid mutator transaction binding the contract method 0x9a11b2e8.
//
// Solidity: function applyKycState(address account, bool remove, uint256 state) returns()
func (_Build *BuildTransactorSession) ApplyKycState(account common.Address, remove bool, state *big.Int) (*types.Transaction, error) {
	return _Build.Contract.ApplyKycState(&_Build.TransactOpts, account, remove, state)
}

// GrantRole is a paid mutator transaction binding the contract method 0x3c09e2fd.
//
// Solidity: function grantRole(address addr, uint256 role) returns()
func (_Build *BuildTransactor) GrantRole(opts *bind.TransactOpts, addr common.Address, role *big.Int) (*types.Transaction, error) {
	return _Build.contract.Transact(opts, "grantRole", addr, role)
}

// GrantRole is a paid mutator transaction binding the contract method 0x3c09e2fd.
//
// Solidity: function grantRole(address addr, uint256 role) returns()
func (_Build *BuildSession) GrantRole(addr common.Address, role *big.Int) (*types.Transaction, error) {
	return _Build.Contract.GrantRole(&_Build.TransactOpts, addr, role)
}

// GrantRole is a paid mutator transaction binding the contract method 0x3c09e2fd.
//
// Solidity: function grantRole(address addr, uint256 role) returns()
func (_Build *BuildTransactorSession) GrantRole(addr common.Address, role *big.Int) (*types.Transaction, error) {
	return _Build.Contract.GrantRole(&_Build.TransactOpts, addr, role)
}

// RevokeRole is a paid mutator transaction binding the contract method 0x0912ed77.
//
// Solidity: function revokeRole(address addr, uint256 role) returns()
func (_Build *BuildTransactor) RevokeRole(opts *bind.TransactOpts, addr common.Address, role *big.Int) (*types.Transaction, error) {
	return _Build.contract.Transact(opts, "revokeRole", addr, role)
}

// RevokeRole is a paid mutator transaction binding the contract method 0x0912ed77.
//
// Solidity: function revokeRole(address addr, uint256 role) returns()
func (_Build *BuildSession) RevokeRole(addr common.Address, role *big.Int) (*types.Transaction, error) {
	return _Build.Contract.RevokeRole(&_Build.TransactOpts, addr, role)
}

// RevokeRole is a paid mutator transaction binding the contract method 0x0912ed77.
//
// Solidity: function revokeRole(address addr, uint256 role) returns()
func (_Build *BuildTransactorSession) RevokeRole(addr common.Address, role *big.Int) (*types.Transaction, error) {
	return _Build.Contract.RevokeRole(&_Build.TransactOpts, addr, role)
}

// SetBaseFee is a paid mutator transaction binding the contract method 0x46860698.
//
// Solidity: function setBaseFee(uint256 newFee) returns()
func (_Build *BuildTransactor) SetBaseFee(opts *bind.TransactOpts, newFee *big.Int) (*types.Transaction, error) {
	return _Build.contract.Transact(opts, "setBaseFee", newFee)
}

// SetBaseFee is a paid mutator transaction binding the contract method 0x46860698.
//
// Solidity: function setBaseFee(uint256 newFee) returns()
func (_Build *BuildSession) SetBaseFee(newFee *big.Int) (*types.Transaction, error) {
	return _Build.Contract.SetBaseFee(&_Build.TransactOpts, newFee)
}

// SetBaseFee is a paid mutator transaction binding the contract method 0x46860698.
//
// Solidity: function setBaseFee(uint256 newFee) returns()
func (_Build *BuildTransactorSession) SetBaseFee(newFee *big.Int) (*types.Transaction, error) {
	return _Build.Contract.SetBaseFee(&_Build.TransactOpts, newFee)
}

// SetBlacklistState is a paid mutator transaction binding the contract method 0xfdff0b26.
//
// Solidity: function setBlacklistState(address account, bytes4 signature, uint256 state) returns()
func (_Build *BuildTransactor) SetBlacklistState(opts *bind.TransactOpts, account common.Address, signature [4]byte, state *big.Int) (*types.Transaction, error) {
	return _Build.contract.Transact(opts, "setBlacklistState", account, signature, state)
}

// SetBlacklistState is a paid mutator transaction binding the contract method 0xfdff0b26.
//
// Solidity: function setBlacklistState(address account, bytes4 signature, uint256 state) returns()
func (_Build *BuildSession) SetBlacklistState(account common.Address, signature [4]byte, state *big.Int) (*types.Transaction, error) {
	return _Build.Contract.SetBlacklistState(&_Build.TransactOpts, account, signature, state)
}

// SetBlacklistState is a paid mutator transaction binding the contract method 0xfdff0b26.
//
// Solidity: function setBlacklistState(address account, bytes4 signature, uint256 state) returns()
func (_Build *BuildTransactorSession) SetBlacklistState(account common.Address, signature [4]byte, state *big.Int) (*types.Transaction, error) {
	return _Build.Contract.SetBlacklistState(&_Build.TransactOpts, account, signature, state)
}

// Upgrade is a paid mutator transaction binding the contract method 0x0900f010.
//
// Solidity: function upgrade(address newImplementation) returns()
func (_Build *BuildTransactor) Upgrade(opts *bind.TransactOpts, newImplementation common.Address) (*types.Transaction, error) {
	return _Build.contract.Transact(opts, "upgrade", newImplementation)
}

// Upgrade is a paid mutator transaction binding the contract method 0x0900f010.
//
// Solidity: function upgrade(address newImplementation) returns()
func (_Build *BuildSession) Upgrade(newImplementation common.Address) (*types.Transaction, error) {
	return _Build.Contract.Upgrade(&_Build.TransactOpts, newImplementation)
}

// Upgrade is a paid mutator transaction binding the contract method 0x0900f010.
//
// Solidity: function upgrade(address newImplementation) returns()
func (_Build *BuildTransactorSession) Upgrade(newImplementation common.Address) (*types.Transaction, error) {
	return _Build.Contract.Upgrade(&_Build.TransactOpts, newImplementation)
}

// BuildDropRoleIterator is returned from FilterDropRole and is used to iterate over the raw logs and unpacked data for DropRole events raised by the Build contract.
type BuildDropRoleIterator struct {
	Event *BuildDropRole // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BuildDropRoleIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BuildDropRole)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BuildDropRole)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BuildDropRoleIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BuildDropRoleIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BuildDropRole represents a DropRole event raised by the Build contract.
type BuildDropRole struct {
	Addr common.Address
	Role *big.Int
	Raw  types.Log // Blockchain specific contextual infos
}

// FilterDropRole is a free log retrieval operation binding the contract event 0xcfa5316bd1be4ceb62f363b0a162f322c33ba870641138cd8600dd4fa603fc3b.
//
// Solidity: event DropRole(address addr, uint256 role)
func (_Build *BuildFilterer) FilterDropRole(opts *bind.FilterOpts) (*BuildDropRoleIterator, error) {

	logs, sub, err := _Build.contract.FilterLogs(opts, "DropRole")
	if err != nil {
		return nil, err
	}
	return &BuildDropRoleIterator{contract: _Build.contract, event: "DropRole", logs: logs, sub: sub}, nil
}

// WatchDropRole is a free log subscription operation binding the contract event 0xcfa5316bd1be4ceb62f363b0a162f322c33ba870641138cd8600dd4fa603fc3b.
//
// Solidity: event DropRole(address addr, uint256 role)
func (_Build *BuildFilterer) WatchDropRole(opts *bind.WatchOpts, sink chan<- *BuildDropRole) (event.Subscription, error) {

	logs, sub, err := _Build.contract.WatchLogs(opts, "DropRole")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BuildDropRole)
				if err := _Build.contract.UnpackLog(event, "DropRole", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDropRole is a log parse operation binding the contract event 0xcfa5316bd1be4ceb62f363b0a162f322c33ba870641138cd8600dd4fa603fc3b.
//
// Solidity: event DropRole(address addr, uint256 role)
func (_Build *BuildFilterer) ParseDropRole(log types.Log) (*BuildDropRole, error) {
	event := new(BuildDropRole)
	if err := _Build.contract.UnpackLog(event, "DropRole", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BuildGasFeeSetIterator is returned from FilterGasFeeSet and is used to iterate over the raw logs and unpacked data for GasFeeSet events raised by the Build contract.
type BuildGasFeeSetIterator struct {
	Event *BuildGasFeeSet // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BuildGasFeeSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BuildGasFeeSet)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BuildGasFeeSet)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BuildGasFeeSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BuildGasFeeSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BuildGasFeeSet represents a GasFeeSet event raised by the Build contract.
type BuildGasFeeSet struct {
	NewGasFee *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterGasFeeSet is a free log retrieval operation binding the contract event 0x1c053aa9b674900648619554980ac10e913d661c372ca30455e1e4ec0ce44071.
//
// Solidity: event GasFeeSet(uint256 newGasFee)
func (_Build *BuildFilterer) FilterGasFeeSet(opts *bind.FilterOpts) (*BuildGasFeeSetIterator, error) {

	logs, sub, err := _Build.contract.FilterLogs(opts, "GasFeeSet")
	if err != nil {
		return nil, err
	}
	return &BuildGasFeeSetIterator{contract: _Build.contract, event: "GasFeeSet", logs: logs, sub: sub}, nil
}

// WatchGasFeeSet is a free log subscription operation binding the contract event 0x1c053aa9b674900648619554980ac10e913d661c372ca30455e1e4ec0ce44071.
//
// Solidity: event GasFeeSet(uint256 newGasFee)
func (_Build *BuildFilterer) WatchGasFeeSet(opts *bind.WatchOpts, sink chan<- *BuildGasFeeSet) (event.Subscription, error) {

	logs, sub, err := _Build.contract.WatchLogs(opts, "GasFeeSet")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BuildGasFeeSet)
				if err := _Build.contract.UnpackLog(event, "GasFeeSet", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseGasFeeSet is a log parse operation binding the contract event 0x1c053aa9b674900648619554980ac10e913d661c372ca30455e1e4ec0ce44071.
//
// Solidity: event GasFeeSet(uint256 newGasFee)
func (_Build *BuildFilterer) ParseGasFeeSet(log types.Log) (*BuildGasFeeSet, error) {
	event := new(BuildGasFeeSet)
	if err := _Build.contract.UnpackLog(event, "GasFeeSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BuildKycStateChangedIterator is returned from FilterKycStateChanged and is used to iterate over the raw logs and unpacked data for KycStateChanged events raised by the Build contract.
type BuildKycStateChangedIterator struct {
	Event *BuildKycStateChanged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BuildKycStateChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BuildKycStateChanged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BuildKycStateChanged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BuildKycStateChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BuildKycStateChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BuildKycStateChanged represents a KycStateChanged event raised by the Build contract.
type BuildKycStateChanged struct {
	Account  common.Address
	OldState *big.Int
	NewState *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterKycStateChanged is a free log retrieval operation binding the contract event 0xf64784c1c207eed151b4adc53adde03b1c3a4ecad6b8de3a65539d464b3e1add.
//
// Solidity: event KycStateChanged(address indexed account, uint256 oldState, uint256 newState)
func (_Build *BuildFilterer) FilterKycStateChanged(opts *bind.FilterOpts, account []common.Address) (*BuildKycStateChangedIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Build.contract.FilterLogs(opts, "KycStateChanged", accountRule)
	if err != nil {
		return nil, err
	}
	return &BuildKycStateChangedIterator{contract: _Build.contract, event: "KycStateChanged", logs: logs, sub: sub}, nil
}

// WatchKycStateChanged is a free log subscription operation binding the contract event 0xf64784c1c207eed151b4adc53adde03b1c3a4ecad6b8de3a65539d464b3e1add.
//
// Solidity: event KycStateChanged(address indexed account, uint256 oldState, uint256 newState)
func (_Build *BuildFilterer) WatchKycStateChanged(opts *bind.WatchOpts, sink chan<- *BuildKycStateChanged, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Build.contract.WatchLogs(opts, "KycStateChanged", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BuildKycStateChanged)
				if err := _Build.contract.UnpackLog(event, "KycStateChanged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseKycStateChanged is a log parse operation binding the contract event 0xf64784c1c207eed151b4adc53adde03b1c3a4ecad6b8de3a65539d464b3e1add.
//
// Solidity: event KycStateChanged(address indexed account, uint256 oldState, uint256 newState)
func (_Build *BuildFilterer) ParseKycStateChanged(log types.Log) (*BuildKycStateChanged, error) {
	event := new(BuildKycStateChanged)
	if err := _Build.contract.UnpackLog(event, "KycStateChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BuildSetRoleIterator is returned from FilterSetRole and is used to iterate over the raw logs and unpacked data for SetRole events raised by the Build contract.
type BuildSetRoleIterator struct {
	Event *BuildSetRole // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BuildSetRoleIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BuildSetRole)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BuildSetRole)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BuildSetRoleIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BuildSetRoleIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BuildSetRole represents a SetRole event raised by the Build contract.
type BuildSetRole struct {
	Addr common.Address
	Role *big.Int
	Raw  types.Log // Blockchain specific contextual infos
}

// FilterSetRole is a free log retrieval operation binding the contract event 0x385a9c70004a48177c93b74796d77d5ebf7e1248f9e2369624514da454cd01b0.
//
// Solidity: event SetRole(address addr, uint256 role)
func (_Build *BuildFilterer) FilterSetRole(opts *bind.FilterOpts) (*BuildSetRoleIterator, error) {

	logs, sub, err := _Build.contract.FilterLogs(opts, "SetRole")
	if err != nil {
		return nil, err
	}
	return &BuildSetRoleIterator{contract: _Build.contract, event: "SetRole", logs: logs, sub: sub}, nil
}

// WatchSetRole is a free log subscription operation binding the contract event 0x385a9c70004a48177c93b74796d77d5ebf7e1248f9e2369624514da454cd01b0.
//
// Solidity: event SetRole(address addr, uint256 role)
func (_Build *BuildFilterer) WatchSetRole(opts *bind.WatchOpts, sink chan<- *BuildSetRole) (event.Subscription, error) {

	logs, sub, err := _Build.contract.WatchLogs(opts, "SetRole")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BuildSetRole)
				if err := _Build.contract.UnpackLog(event, "SetRole", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSetRole is a log parse operation binding the contract event 0x385a9c70004a48177c93b74796d77d5ebf7e1248f9e2369624514da454cd01b0.
//
// Solidity: event SetRole(address addr, uint256 role)
func (_Build *BuildFilterer) ParseSetRole(log types.Log) (*BuildSetRole, error) {
	event := new(BuildSetRole)
	if err := _Build.contract.UnpackLog(event, "SetRole", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
