package vm

import "testing"

func TestPrecompiledContractSpendsGas(t *testing.T) {
	unwrapped := &sha256hash{}

	input := []byte{'J', 'E', 'T', 'S'}
	requiredGas := unwrapped.RequiredGas(input)
	_, remainingGas, err := RunPrecompiledContract(unwrapped, input, requiredGas)
	if err != nil {
		t.Fatalf("Unexpectedly failed to run precompiled contract: %s", err)
	}

	if remainingGas != 0 {
		t.Fatalf("Found more remaining gas than expected: %d", remainingGas)
	}
}
