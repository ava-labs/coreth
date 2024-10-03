package evm

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/log"
)

func (vm *VM) script() error {
	vm.blockChain.Stop()

	vmConfig := vm.blockChain.GetVMConfig()
	cacheConfig := *vm.blockChain.GetCacheConfig()
	// Disable snapshotting
	cacheConfig.SnapshotDelayInit = true
	cacheConfig.SnapshotLimit = 0

	chain, err := core.NewBlockChain(
		vm.chaindb,
		&cacheConfig,
		vm.ethConfig.Genesis,
		vm.blockChain.Engine(),
		*vmConfig,
		vm.blockChain.LastAcceptedBlock().Hash(),
		false)
	if err != nil {
		return fmt.Errorf("failed to create new blockchain: %w", err)
	}

	var wg sync.WaitGroup
	progress := make(chan *types.Block, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for block := range progress {
			log.Info("Reprocessed block", "number", block.Number(), "hash", block.Hash())
		}
	}()
	if err := chain.Reprocess(1, 4096, progress); err != nil {
		return fmt.Errorf("failed to reprocess blockchain: %w", err)
	}
	close(progress)
	wg.Wait()

	chain.Stop()

	return errors.New("intentionally stopping VM from initializing")
}
