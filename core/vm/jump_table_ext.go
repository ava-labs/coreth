// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import "github.com/ethereum/go-ethereum/params"

const (
	BALANCEMC = 0xcd
	CALLEX    = 0xcf
)

var (
	apricotPhase1InstructionSet = newApricotPhase1InstructionSet()
	apricotPhase2InstructionSet = newApricotPhase2InstructionSet()
	apricotPhase3InstructionSet = newApricotPhase3InstructionSet()
	durangoInstructionSet       = newDurangoInstructionSet()
)

func newDurangoInstructionSet() JumpTable {
	instructionSet := newApricotPhase3InstructionSet()
	enable3855(&instructionSet) // PUSH0 instruction
	enable3860(&instructionSet) // Limit and meter initcode
	return validate(instructionSet)
}

// newApricotPhase3InstructionSet returns the frontier, homestead, byzantium,
// constantinople, istanbul, petersburg, apricotPhase1, 2, and 3 instructions.
func newApricotPhase3InstructionSet() JumpTable {
	instructionSet := newApricotPhase2InstructionSet()
	enable3198(&instructionSet) // Base fee opcode https://eips.ethereum.org/EIPS/eip-3198
	return validate(instructionSet)
}

// newApricotPhase1InstructionSet returns the frontier,
// homestead, byzantium, constantinople petersburg,
// istanbul, and apricotPhase1 instructions.
func newApricotPhase2InstructionSet() JumpTable {
	instructionSet := newApricotPhase1InstructionSet()

	enable2929(&instructionSet)
	enableAP2(&instructionSet)

	return validate(instructionSet)
}

// newApricotPhase1InstructionSet returns the frontier,
// homestead, byzantium, constantinople petersburg,
// and istanbul instructions.
func newApricotPhase1InstructionSet() JumpTable {
	instructionSet := newIstanbulInstructionSet()
	enableHomestead_ext(&instructionSet)
	enableEIP150_ext(&instructionSet)
	enableAP1(&instructionSet)

	return validate(instructionSet)
}

func enableEIP150_ext(jt *JumpTable) {
	jt[CALLEX].constantGas = params.CallGasEIP150
}

func enableHomestead_ext(jt *JumpTable) {
	opCodeToString[BALANCEMC] = "BALANCEMC"
	opCodeToString[CALLEX] = "CALLEX"
	stringToOp["BALANCEMC"] = BALANCEMC
	stringToOp["CALLEX"] = CALLEX

	jt[BALANCEMC] = &operation{
		execute:     opBalanceMultiCoin,
		constantGas: params.BalanceGasFrontier,
		minStack:    minStack(2, 1),
		maxStack:    maxStack(2, 1),
	}
	jt[CALLEX] = &operation{
		execute:     opCallExpert,
		constantGas: params.CallGasFrontier,
		dynamicGas:  gasCall,
		minStack:    minStack(9, 1),
		maxStack:    maxStack(9, 1),
		memorySize:  memoryCallExpert,
	}
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

// enableAP1 disables gas refunds for SSTORE and SELFDESTRUCT. It is very
// similar to EIP-3298: Removal of Refunds [DRAFT]
// (https://eips.ethereum.org/EIPS/eip-3298).
func enableAP1(jt *JumpTable) {
	jt[SSTORE].dynamicGas = gasSStoreAP1
	jt[SELFDESTRUCT].dynamicGas = gasSelfdestructAP1
	jt[CALLEX].dynamicGas = gasCallExpertAP1
}

func enableAP2(jt *JumpTable) {
	jt[SSTORE].dynamicGas = _gasSStoreEIP2929
	jt[SELFDESTRUCT].dynamicGas = _gasSelfdestructEIP2929
	jt[BALANCEMC] = &operation{execute: opUndefined, maxStack: maxStack(0, 0)}
	jt[CALLEX] = &operation{execute: opUndefined, maxStack: maxStack(0, 0)}
}
