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
	"math/big"

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

	// Set of all native precompile addresses that are in use
	// Note: this will repeat some addresses, but this is cheap and makes the code clearer.
	PrecompileAllNativeAddresses = make(map[common.Address]struct{})
	addrsList := make([]common.Address, 0)
	addrsList = append(addrsList, vm.PrecompiledAddressesIstanbul...)
	addrsList = append(addrsList, PrecompiledAddressesApricotPhase2...)
	addrsList = append(addrsList, PrecompiledAddressesApricotPhasePre6...)
	addrsList = append(addrsList, PrecompiledAddressesApricotPhase6...)
	addrsList = append(addrsList, PrecompiledAddressesBanff...)
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
	switch {
	case rules.IsBanff:
		return PrecompiledAddressesBanff
	case rules.IsApricotPhase2:
		return PrecompiledAddressesApricotPhase2
	case rules.IsIstanbul:
		return vm.PrecompiledAddressesIstanbul
	case rules.IsByzantium:
		return vm.PrecompiledAddressesByzantium
	default:
		return vm.PrecompiledAddressesHomestead
	}
}

var (
	big0 = big.NewInt(0)
)
