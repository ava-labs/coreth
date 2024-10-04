package evm

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/log"
)

func (vm *VM) traceBlock() error {
	number := uint64(0)
	if env := os.Getenv("BLOCK_REPROCESS_TRACE"); env != "" {
		parsed, err := strconv.Atoi(env)
		if err != nil {
			return err
		}
		number = uint64(parsed)
	}
	if number == 0 {
		return nil // contiune
	}

	ft := tracers.NewFileTracerAPI(vm.eth.APIBackend)
	block := vm.blockChain.GetBlockByNumber(number)
	outs, err := ft.StandardTraceBlockToFile(
		context.Background(),
		block.Hash(),
		&tracers.StdTraceConfig{
			Reexec: utils.NewUint64(4096),
		},
	)
	log.Info("tracing block", "block", number, "err", err)
	for _, out := range outs {
		log.Info("tracing output", "block", number, "out", out)
	}
	return errors.New("intentionally stopping VM from initializing (trace)")
}
