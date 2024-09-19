// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

// newApricotPhase1InstructionSet returns the frontier,
// homestead, byzantium, constantinople petersburg,
// and istanbul instructions.
func newApricotPhase1InstructionSet() JumpTable {
	instructionSet := newIstanbulInstructionSet()

	enableAP1(&instructionSet)

	return validate(instructionSet)
}

func enableAP1(jt *JumpTable) {
	jt[SSTORE].dynamicGas = gasSStoreAP1
	jt[SELFDESTRUCT].dynamicGas = gasSelfdestructAP1
}
