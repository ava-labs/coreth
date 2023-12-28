// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"errors"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

// gasSStoreEIP2929 implements gas cost for SSTORE according to EIP-2929
//
// When calling SSTORE, check if the (address, storage_key) pair is in accessed_storage_keys.
// If it is not, charge an additional COLD_SLOAD_COST gas, and add the pair to accessed_storage_keys.
// Additionally, modify the parameters defined in EIP 2200 as follows:
//
// Parameter 	Old value 	New value
// SLOAD_GAS 	800 	= WARM_STORAGE_READ_COST
// SSTORE_RESET_GAS 	5000 	5000 - COLD_SLOAD_COST
//
// The other parameters defined in EIP 2200 are unchanged.
// see gasSStoreEIP2200(...) in core/vm/gas_table.go for more info about how EIP 2200 is specified
func gasSStoreEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	// If we fail the minimum gas availability invariant, fail (0)
	if contract.Gas <= params.SstoreSentryGasEIP2200 {
		return 0, errors.New("not enough gas for reentrancy sentry")
	}
	// Gas sentry honoured, do the actual gas calculation based on the stored value
	var (
		y, x    = stack.Back(1), stack.peek()
		slot    = common.Hash(x.Bytes32())
		current = evm.StateDB.GetState(contract.Address(), slot)
		cost    = uint64(0)
	)
	// Check slot presence in the access list
	if addrPresent, slotPresent := evm.StateDB.SlotInAccessList(contract.Address(), slot); !slotPresent {
		cost = params.ColdSloadCostEIP2929
		// If the caller cannot afford the cost, this change will be rolled back
		evm.StateDB.AddSlotToAccessList(contract.Address(), slot)
		if !addrPresent {
			// Once we're done with YOLOv2 and schedule this for mainnet, might
			// be good to remove this panic here, which is just really a
			// canary to have during testing
			panic("impossible case: address was not present in access list during sstore op")
		}
	}
	value := common.Hash(y.Bytes32())

	if current == value { // noop (1)
		// EIP 2200 original clause:
		//		return params.SloadGasEIP2200, nil
		return cost + params.WarmStorageReadCostEIP2929, nil // SLOAD_GAS
	}
	original := evm.StateDB.GetCommittedStateAP1(contract.Address(), x.Bytes32())
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return cost + params.SstoreSetGasEIP2200, nil
		}
		// EIP-2200 original clause:
		//		return params.SstoreResetGasEIP2200, nil // write existing slot (2.1.2)
		return cost + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929), nil // write existing slot (2.1.2)
	}

	// EIP-2200 original clause:
	//return params.SloadGasEIP2200, nil // dirty update (2.2)
	return cost + params.WarmStorageReadCostEIP2929, nil // dirty update (2.2)
}

func gasSelfdestructEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	var (
		gas     uint64
		address = common.Address(stack.peek().Bytes20())
	)
	if !evm.StateDB.AddressInAccessList(address) {
		// If the caller cannot afford the cost, this change will be rolled back
		evm.StateDB.AddAddressToAccessList(address)
		gas = params.ColdAccountAccessCostEIP2929
	}
	// if empty and transfers value
	if evm.StateDB.Empty(address) && evm.StateDB.GetBalance(contract.Address()).Sign() != 0 {
		gas += params.CreateBySelfdestructGas
	}

	return gas, nil
}

// gasSStoreAP1 simplifies the dynamic gas cost of SSTORE by removing all refund logic
//
//  0. If *gasleft* is less than or equal to 2300, fail the current call.
//  1. If current value equals new value (this is a no-op), SLOAD_GAS is deducted.
//  2. If current value does not equal new value:
//     2.1. If original value equals current value (this storage slot has not been changed by the current execution context):
//     2.1.1. If original value is 0, SSTORE_SET_GAS (20K) gas is deducted.
//     2.1.2. Otherwise, SSTORE_RESET_GAS gas is deducted. If new value is 0, add SSTORE_CLEARS_SCHEDULE to refund counter.
//     2.2. If original value does not equal current value (this storage slot is dirty), SLOAD_GAS gas is deducted. Apply both of the following clauses:
func gasSStoreAP1(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	// If we fail the minimum gas availability invariant, fail (0)
	if contract.Gas <= params.SstoreSentryGasEIP2200 {
		return 0, errors.New("not enough gas for reentrancy sentry")
	}
	// Gas sentry honoured, do the actual gas calculation based on the stored value
	var (
		y, x    = stack.Back(1), stack.Back(0)
		current = evm.StateDB.GetState(contract.Address(), x.Bytes32())
	)
	value := common.Hash(y.Bytes32())

	if current == value { // noop (1)
		return params.SloadGasEIP2200, nil
	}
	original := evm.StateDB.GetCommittedStateAP1(contract.Address(), x.Bytes32())
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return params.SstoreSetGasEIP2200, nil
		}
		return params.SstoreResetGasEIP2200, nil // write existing slot (2.1.2)
	}

	return params.SloadGasEIP2200, nil // dirty update (2.2)
}

func gasCallExpertAP1(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	var (
		gas                     uint64
		transfersValue          = !stack.Back(2).IsZero()
		multiCoinTransfersValue = !stack.Back(4).IsZero()
		address                 = common.Address(stack.Back(1).Bytes20())
	)
	if evm.chainRules.IsEIP158 {
		if (transfersValue || multiCoinTransfersValue) && evm.StateDB.Empty(address) {
			gas += params.CallNewAccountGas
		}
	} else if !evm.StateDB.Exist(address) {
		gas += params.CallNewAccountGas
	}
	if transfersValue {
		gas += params.CallValueTransferGas
	}
	if multiCoinTransfersValue {
		gas += params.CallValueTransferGas
	}
	memoryGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	var overflow bool
	if gas, overflow = math.SafeAdd(gas, memoryGas); overflow {
		return 0, ErrGasUintOverflow
	}

	evm.callGasTemp, err = callGas(evm.chainRules.IsEIP150, contract.Gas, gas, stack.Back(0))
	if err != nil {
		return 0, err
	}
	if gas, overflow = math.SafeAdd(gas, evm.callGasTemp); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

func gasSelfdestructAP1(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	var gas uint64
	// EIP150 homestead gas reprice fork:
	if evm.chainRules.IsEIP150 {
		gas = params.SelfdestructGasEIP150
		var address = common.Address(stack.Back(0).Bytes20())

		if evm.chainRules.IsEIP158 {
			// if empty and transfers value
			if evm.StateDB.Empty(address) && evm.StateDB.GetBalance(contract.Address()).Sign() != 0 {
				gas += params.CreateBySelfdestructGas
			}
		} else if !evm.StateDB.Exist(address) {
			gas += params.CreateBySelfdestructGas
		}
	}

	return gas, nil
}
