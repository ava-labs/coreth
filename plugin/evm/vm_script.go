package evm

import (
	"errors"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/log"
)

func (vm *VM) script() error {
	vm.blockChain.Stop()

	var parent *types.Block
	for i := uint64(1); i < vm.blockChain.LastAcceptedBlock().NumberU64(); i++ {
		block := vm.blockChain.GetBlockByNumber(i)
		if parent == nil {
			parent = vm.blockChain.GetBlock(block.ParentHash(), i-1)
		}

		if parent.Root() == block.Root() {
			log.Warn("Block has the same root as parent", "block", block.NumberU64(), "root", block.Root(), "parent", parent.Root())
		}
		if i%100_000 == 0 {
			log.Info("Processed block", "block", i)
		}

		parent = block
	}

	return errors.New("intentionally stopping vm")
}
