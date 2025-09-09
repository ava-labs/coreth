package evm

import (
	"slices"

	"github.com/ava-labs/libevm/common"

	// The sole intent of this file is to hand-craft EVM assembly, which makes
	// extensive use of the vm.Code constants. Usage of dot-import here MUST NOT
	// be construed as precedent for doing so elsewhere.
	. "github.com/ava-labs/libevm/core/vm"
)

func delegateCallInConstructor(addr common.Address) []byte {
	contract := slices.Concat([][]OpCode{
		{
			PUSH0, // retSize
			PUSH0, // retOffset
			PUSH0, // argsSize
			PUSH0, // argsOffset
		},
		append([]OpCode{PUSH20}, changeByteType[byte, OpCode](addr.Bytes())...),
		{
			PUSH2, 0xFF, 0xFF, // gas (arbitrary but sufficient to succeed when not reverting)
			DELEGATECALL, // stack will now be 1 / 0 for success / revert, respectively
		},
		{
			PUSH1, 0x23, // JUMPDEST
			JUMPI,
			PUSH0, PUSH0, REVERT, // False branch; bubble up the revert
			JUMPDEST, STOP, // True branch; clean exit
		},
	}...)
	return changeByteType[OpCode, byte](contract)
}

func changeByteType[From ~byte, To ~byte](bs []From) []To {
	out := make([]To, len(bs))
	for i, b := range bs {
		out[i] = To(b)
	}
	return out
}
