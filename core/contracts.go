// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// PrecompiledContract is the basic interface for native Go contracts. The implementation
// requires a deterministic gas count based on the input size of the Run method of the
// contract.
type PrecompiledContract interface {
	RequiredGas(input []byte) uint64  // RequiredPrice calculates the contract gas use
	Run(input []byte) ([]byte, error) // Run runs the precompiled contract
}

// PrecompiledContractsApricotPhase2 contains the default set of pre-compiled Ethereum
// contracts used in the Apricot Phase 2 release.
var PrecompiledContractsApricotPhase2 = map[common.Address]contract.StatefulPrecompiledContract{
	genesisContractAddr:    &deprecatedContract{},
	NativeAssetBalanceAddr: &nativeAssetBalance{gasCost: params.AssetBalanceApricot},
	NativeAssetCallAddr:    &nativeAssetCall{gasCost: params.AssetCallApricot},
}

// PrecompiledContractsApricotPhasePre6 contains the default set of pre-compiled Ethereum
// contracts used in the PrecompiledContractsApricotPhasePre6 release.
var PrecompiledContractsApricotPhasePre6 = map[common.Address]contract.StatefulPrecompiledContract{
	genesisContractAddr:    &deprecatedContract{},
	NativeAssetBalanceAddr: &deprecatedContract{},
	NativeAssetCallAddr:    &deprecatedContract{},
}

// PrecompiledContractsApricotPhase6 contains the default set of pre-compiled Ethereum
// contracts used in the Apricot Phase 6 release.
var PrecompiledContractsApricotPhase6 = map[common.Address]contract.StatefulPrecompiledContract{
	genesisContractAddr:    &deprecatedContract{},
	NativeAssetBalanceAddr: &nativeAssetBalance{gasCost: params.AssetBalanceApricot},
	NativeAssetCallAddr:    &nativeAssetCall{gasCost: params.AssetCallApricot},
}

// PrecompiledContractsBanff contains the default set of pre-compiled Ethereum
// contracts used in the Banff release.
var PrecompiledContractsBanff = map[common.Address]contract.StatefulPrecompiledContract{
	genesisContractAddr:    &deprecatedContract{},
	NativeAssetBalanceAddr: &deprecatedContract{},
	NativeAssetCallAddr:    &deprecatedContract{},
}

var (
	PrecompiledAddressesBanff            []common.Address
	PrecompiledAddressesApricotPhase6    []common.Address
	PrecompiledAddressesApricotPhasePre6 []common.Address
	PrecompiledAddressesApricotPhase2    []common.Address
	PrecompiledAddressesBLS              []common.Address
	PrecompileAllNativeAddresses         map[common.Address]struct{}
)

func init() {
	for k := range PrecompiledContractsApricotPhase2 {
		PrecompiledAddressesApricotPhase2 = append(PrecompiledAddressesApricotPhase2, k)
	}
	for k := range PrecompiledContractsApricotPhasePre6 {
		PrecompiledAddressesApricotPhasePre6 = append(PrecompiledAddressesApricotPhasePre6, k)
	}
	for k := range PrecompiledContractsApricotPhase6 {
		PrecompiledAddressesApricotPhase6 = append(PrecompiledAddressesApricotPhase6, k)
	}
	for k := range PrecompiledContractsBanff {
		PrecompiledAddressesBanff = append(PrecompiledAddressesBanff, k)
	}
	for k := range vm.PrecompiledContractsBLS {
		PrecompiledAddressesBLS = append(PrecompiledAddressesBLS, k)
	}

	// Set of all native precompile addresses that are in use
	// Note: this will repeat some addresses, but this is cheap and makes the code clearer.
	PrecompileAllNativeAddresses = make(map[common.Address]struct{})
	addrsList := append(vm.PrecompiledAddressesHomestead, vm.PrecompiledAddressesByzantium...)
	addrsList = append(addrsList, vm.PrecompiledAddressesIstanbul...)
	addrsList = append(addrsList, PrecompiledAddressesApricotPhase2...)
	addrsList = append(addrsList, PrecompiledAddressesApricotPhasePre6...)
	addrsList = append(addrsList, PrecompiledAddressesApricotPhase6...)
	addrsList = append(addrsList, PrecompiledAddressesBanff...)
	addrsList = append(addrsList, vm.PrecompiledAddressesCancun...)
	addrsList = append(addrsList, PrecompiledAddressesBLS...)
	for _, k := range addrsList {
		PrecompileAllNativeAddresses[k] = struct{}{}
	}

	// Ensure that this package will panic during init if there is a conflict present with the declared
	// precompile addresses.
	for _, module := range modules.RegisteredModules() {
		address := module.Address
		if _, ok := PrecompileAllNativeAddresses[address]; ok {
			panic(fmt.Errorf("precompile address collides with existing native address: %s", address))
		}
	}
}

// ActivePrecompiles returns the precompiles enabled with the current configuration.
func ActivePrecompiles(rules params.Rules) []common.Address {
	var precompiles []common.Address
	precompiles = append(precompiles, vm.ActivePrecompiles(asGethRules(rules))...)
	precompiles = append(precompiles, avalanchePrecompiles(rules)...)
	return precompiles
}

// avalanchePrecompiles returns the Avalanche specific precompiles enabled with
// the current configuration.
func avalanchePrecompiles(rules params.Rules) []common.Address {
	switch {
	case rules.IsBanff:
		return PrecompiledAddressesBanff
	case rules.IsApricotPhase6:
		return PrecompiledAddressesApricotPhase6
	case rules.IsApricotPhasePre6:
		return PrecompiledAddressesApricotPhasePre6
	case rules.IsApricotPhase2:
		return PrecompiledAddressesApricotPhase2
	default:
		return nil
	}
}
