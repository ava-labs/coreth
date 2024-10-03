package evm

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/sync/errgroup"
)

func (vm *VM) script() error {
	vm.blockChain.Stop()

	vmConfig := vm.blockChain.GetVMConfig()
	cacheConfig := *vm.blockChain.GetCacheConfig()
	// Disable snapshotting
	cacheConfig.SnapshotDelayInit = true
	cacheConfig.SnapshotLimit = 0

	progress := make(chan *types.Block, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var (
			processed     uint64
			lastProcessed uint64
			start         = time.Now()
			last          = start
			update        = 5 * time.Second
		)
		for range progress {
			processed++
			if time.Since(last) > update {
				last = time.Now()
				log.Info(
					"reprocessing",
					"processed", processed,
					"new", processed-lastProcessed,
					"blocks/s", float64(processed-lastProcessed)/update.Seconds(),
				)
				lastProcessed = processed
			}
		}
		log.Info(
			"reprocessing finished",
			"processed", processed,
			"duration", time.Since(start),
			"blocks/s", float64(processed)/time.Since(start).Seconds(),
		)
	}()

	work := func(from, to uint64) func() error {
		return func() error {
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
			if err := chain.Reprocess(from, to, progress); err != nil {
				return fmt.Errorf("failed to reprocess blockchain: %w", err)
			}
			chain.Stop()
			return nil
		}
	}

	var eg errgroup.Group
	for i := 0; i < 2; i++ {
		from, to := uint64(i)*4096+1, uint64(i+1)*4096
		eg.Go(work(from, to))
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	close(progress)
	wg.Wait()

	return errors.New("intentionally stopping VM from initializing")
}
