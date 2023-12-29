// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

type Multicoiner interface {
	GetBalanceMultiCoin(stateDB StateDB, address common.Address, coinID common.Hash) *big.Int
	CanTransferMC(stateDB StateDB, from common.Address, to common.Address, coinID common.Hash, amount *big.Int) bool
	TransferMultiCoin(stateDB StateDB, from common.Address, to common.Address, coinID common.Hash, amount *big.Int)
	UnpackNativeAssetCallInput(input []byte) (common.Address, common.Hash, *big.Int, []byte, error)
}

func opBalanceMultiCoin(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	addr, cid := scope.Stack.pop(), scope.Stack.pop()
	res, err := uint256.FromBig(
		interpreter.evm.Config.Multicoiner.GetBalanceMultiCoin(
			interpreter.evm.StateDB,
			common.BigToAddress(addr.ToBig()),
			common.BigToHash(cid.ToBig()),
		),
	)
	if err {
		return nil, errors.New("balance overflow")
	}
	scope.Stack.push(res)
	return nil, nil
}

func memoryCallExpert(stack *Stack) (uint64, bool) {
	x, overflow := calcMemSize64(stack.Back(7), stack.Back(8))
	if overflow {
		return 0, true
	}
	y, overflow := calcMemSize64(stack.Back(5), stack.Back(6))
	if overflow {
		return 0, true
	}
	if x > y {
		return x, false
	}
	return y, false
}

// Note: opCallExpert was de-activated in ApricotPhase2.
func opCallExpert(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	stack := scope.Stack
	// Pop gas. The actual gas in interpreter.evm.callGasTemp.
	// We can use this as a temporary value
	temp := stack.pop()
	gas := interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, value, cid, value2, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	toAddr := common.Address(addr.Bytes20())
	coinID := common.BigToHash(cid.ToBig())
	// Get the arguments from the memory.
	args := scope.Memory.GetPtr(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Note: this code fails to check that value2 is zero, which was a bug when CALLEX was active.
	// The CALLEX opcode was de-activated in ApricotPhase2 resolving this issue.
	if interpreter.readOnly && !value.IsZero() {
		return nil, ErrWriteProtection
	}
	var bigVal = big0
	//TODO: use uint256.Int instead of converting with toBig()
	// By using big0 here, we save an alloc for the most common case (non-ether-transferring contract calls),
	// but it would make more sense to extend the usage of uint256.Int
	if !value.IsZero() {
		gas += params.CallStipend
		bigVal = value.ToBig()
	}

	var bigVal2 = big0
	//TODO: use uint256.Int instead of converting with toBig()
	// By using big0 here, we save an alloc for the most common case (non-ether-transferring contract calls),
	// but it would make more sense to extend the usage of uint256.Int
	if !value2.IsZero() {
		bigVal2 = value2.ToBig()
	}

	ret, returnGas, err := interpreter.evm.CallExpert(scope.Contract, toAddr, args, gas, bigVal, coinID, bigVal2)
	if err != nil {
		temp.Clear()
	} else {
		temp.SetOne()
	}
	stack.push(&temp)
	if err == nil || err == ErrExecutionReverted {
		scope.Memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}
	scope.Contract.Gas += returnGas

	interpreter.returnData = ret
	return ret, nil
}

// This allows the user transfer balance of a specified coinId in addition to a normal Call().
func (evm *EVM) CallExpert(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int, coinID common.Hash, value2 *big.Int) (ret []byte, leftOverGas uint64, err error) {
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	// Fail if we're trying to transfer more than the available balance
	// Note: it is not possible for a negative value to be passed in here due to the fact
	// that [value] will be popped from the stack and decoded to a *big.Int, which will
	// always yield a positive result.
	if value.Sign() != 0 && !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}

	if value2.Sign() != 0 && !evm.Config.Multicoiner.CanTransferMC(evm.StateDB, caller.Address(), addr, coinID, value2) {
		return nil, gas, ErrInsufficientBalance
	}

	snapshot := evm.StateDB.Snapshot()
	//p, isPrecompile := evm.precompile(addr)

	if !evm.StateDB.Exist(addr) {
		//if !isPrecompile && evm.chainRules.IsEIP158 && value.Sign() == 0 {
		//	// Calling a non existing account, don't do anything, but ping the tracer
		//	if evm.Config.Debug && evm.depth == 0 {
		//		evm.Config.Tracer.CaptureStart(evm, caller.Address(), addr, false, input, gas, value)
		//		evm.Config.Tracer.CaptureEnd(ret, 0, 0, nil)
		//	}
		//	return nil, gas, nil
		//}
		evm.StateDB.CreateAccount(addr)
	}
	evm.Context.Transfer(evm.StateDB, caller.Address(), addr, value)
	evm.Config.Multicoiner.TransferMultiCoin(evm.StateDB, caller.Address(), addr, coinID, value2)

	// Capture the tracer start/end events in debug mode
	debug := evm.Config.Tracer != nil
	if debug && evm.depth == 0 {
		evm.Config.Tracer.CaptureStart(evm, caller.Address(), addr, false, input, gas, value)
		defer func(startGas uint64, startTime time.Time) { // Lazy evaluation of the parameters
			evm.Config.Tracer.CaptureEnd(ret, startGas-gas, err)
		}(gas, time.Now())
	}

	//if isPrecompile {
	//	ret, gas, err = RunPrecompiledContract(p, input, gas)
	//} else {
	// Initialise a new contract and set the code that is to be used by the EVM.
	// The contract is a scoped environment for this execution context only.
	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		ret, err = nil, nil // gas is unchanged
	} else {
		addrCopy := addr
		// If the account has no code, we can abort here
		// The depth-check is already done, and precompiles handled above
		contract := NewContract(caller, AccountRef(addrCopy), value, gas)
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), code)
		ret, err = evm.interpreter.Run(contract, input, false)
		gas = contract.Gas
	}
	//}
	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, gas, err
}

func (evm *EVM) NativeAssetCall(caller common.Address, input []byte, suppliedGas uint64, gasCost uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if suppliedGas < gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - gasCost

	if readOnly {
		return nil, remainingGas, ErrExecutionReverted
	}

	to, assetID, assetAmount, callData, err := evm.Config.Multicoiner.UnpackNativeAssetCallInput(input)
	if err != nil {
		return nil, remainingGas, ErrExecutionReverted
	}

	// Note: it is not possible for a negative assetAmount to be passed in here due to the fact that decoding a
	// byte slice into a *big.Int type will always return a positive value.
	if assetAmount.Sign() != 0 && !evm.Config.Multicoiner.CanTransferMC(evm.StateDB, caller, to, assetID, assetAmount) {
		return nil, remainingGas, ErrInsufficientBalance
	}

	snapshot := evm.StateDB.Snapshot()

	if !evm.StateDB.Exist(to) {
		if remainingGas < params.CallNewAccountGas {
			return nil, 0, ErrOutOfGas
		}
		remainingGas -= params.CallNewAccountGas
		evm.StateDB.CreateAccount(to)
	}

	// Increment the call depth which is restricted to 1024
	evm.depth++
	defer func() { evm.depth-- }()

	// Send [assetAmount] of [assetID] to [to] address
	evm.Config.Multicoiner.TransferMultiCoin(evm.StateDB, caller, to, assetID, assetAmount)
	ret, remainingGas, err = evm.Call(AccountRef(caller), to, callData, remainingGas, new(big.Int))

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			remainingGas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

var (
	OpBalanceMultiCoin = &operation{
		execute:     opBalanceMultiCoin,
		constantGas: params.BalanceGasFrontier,
		minStack:    minStack(2, 1),
		maxStack:    maxStack(2, 1),
	}
	OpCallExpert = &operation{
		execute:     opCallExpert,
		constantGas: params.CallGasFrontier,
		dynamicGas:  gasCall,
		minStack:    minStack(9, 1),
		maxStack:    maxStack(9, 1),
		memorySize:  memoryCallExpert,
	}
)
