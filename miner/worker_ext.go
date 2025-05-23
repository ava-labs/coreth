// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package miner

import (
	"fmt"
	"maps"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	customheader "github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/log"
)

const (
	// Leaves 256 KBs for other sections of the block (limit is 2MB).
	// This should suffice for atomic txs, proposervm header, and serialization overhead.
	targetTxsSize = 1792 * units.KiB
)

var hooks = &workerHooks{}

type workerHooks struct{}

func (h *workerHooks) minerFee(block *types.Block, tx *types.Transaction) *big.Int {
	baseFee := block.BaseFee()
	if baseFee == nil {
		// Prior to activation of EIP-1559, the coinbase payment was gasPrice * gasUsed
		return tx.GasPrice()
	}
	// Note in coreth the coinbase payment is (baseFee + effectiveGasTip) * gasUsed
	return new(big.Int).Add(baseFee, tx.EffectiveGasTipValue(baseFee))
}

func (h *workerHooks) startRunningValue() bool {
	return false // must be always false
}

func (h *workerHooks) startPrefetcherOpts(config *core.CacheConfig) []state.PrefetcherOption {
	numPrefetchers := config.TriePrefetcherParallelism
	return []state.PrefetcherOption{
		state.WithConcurrentWorkers(numPrefetchers),
	}
}

func (h *workerHooks) makeGasPool(config *params.ChainConfig,
	header, parent *types.Header) (*core.GasPool, error) {
	chainConfigExtra := params.GetExtra(config)
	capacity, err := customheader.GasCapacity(chainConfigExtra, parent, header.Time)
	if err != nil {
		return nil, fmt.Errorf("calculating gas capacity: %w", err)
	}
	pool := new(core.GasPool)
	pool.AddGas(capacity)
	return pool, nil
}

func newEnvironmentHooks(
	parent *types.Header,
	genParamsExtra *generateParamsHooks,
) *environmentHooks {
	var predicateContext *precompileconfig.PredicateContext
	if genParamsExtra != nil {
		predicateContext = genParamsExtra.predicateContext
	}
	return &environmentHooks{
		parent:           parent,
		predicateContext: predicateContext,
		predicateResults: predicate.NewResults(),
	}
}

func (h *workerHooks) prepareWorkFinalizer(config *params.ChainConfig,
	header, parent *types.Header, state *state.StateDB) error {
	// Configure any upgrades that should go into effect during this block.
	blockContext := core.NewBlockContext(header.Number, header.Time)
	err := core.ApplyUpgrades(config, &parent.Time, blockContext, state)
	if err != nil {
		log.Error("failed to configure precompiles mining new block", "parent", parent.Hash(), "number", header.Number, "timestamp", header.Time, "err", err)
		return err
	}
	return nil
}

func (h *workerHooks) handleDurango(chainConfig *params.ChainConfig, header *types.Header,
	envHooks *environmentHooks) *newPayloadResult {
	rules := chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
	rulesExtra := params.GetRulesExtra(rules)
	if !rulesExtra.IsDurango {
		return nil
	}
	predicateResultsBytes, err := envHooks.predicateResults.Bytes()
	if err != nil {
		return &newPayloadResult{err: fmt.Errorf("failed to marshal predicate results: %w", err)}
	}
	header.Extra = append(header.Extra, predicateResultsBytes...)
	return nil
}

type environmentHooks struct {
	parent           *types.Header
	predicateContext *precompileconfig.PredicateContext
	predicateResults *predicate.Results
	size             uint64
}

func (e *environmentHooks) Copy() *environmentHooks {
	return &environmentHooks{
		parent:           types.CopyHeader(e.parent),
		predicateContext: e.predicateContext,
		predicateResults: predicate.NewResultsFromMap(maps.Clone(e.predicateResults.Results)),
		size:             e.size,
	}
}

func (h *environmentHooks) commitTransactionFinalizer(tx *types.Transaction) {
	h.size += tx.Size()
}

func (h *environmentHooks) makeBlockContext(tx *types.Transaction,
	chain *core.BlockChain, envHeader *types.Header,
	coinbase *common.Address) (blockContext vm.BlockContext, err error) {
	rules := chain.Config().Rules(envHeader.Number, params.IsMergeTODO, envHeader.Time)
	rulesExtra := params.GetRulesExtra(rules)

	if !rulesExtra.IsDurango {
		return core.NewEVMBlockContext(envHeader, chain, coinbase), nil
	}

	results, err := core.CheckPredicates(rules, h.predicateContext, tx)
	if err != nil {
		log.Debug("Transaction predicate failed verification in miner", "tx", tx.Hash(), "err", err)
		return vm.BlockContext{}, err
	}
	h.predicateResults.SetTxResults(tx.Hash(), results)

	predicateResultsBytes, err := h.predicateResults.Bytes()
	if err != nil {
		return vm.BlockContext{}, fmt.Errorf("failed to marshal predicate results: %w", err)
	}
	blockContext = core.NewEVMBlockContextWithPredicateResults(rulesExtra.AvalancheRules, envHeader, chain, coinbase, predicateResultsBytes)
	return blockContext, nil
}

func (h *environmentHooks) onApplyTxFailure(txHash common.Hash) {
	h.predicateResults.DeleteTxResults(txHash)
}

func (h *environmentHooks) skipTxToCommit(tx *types.Transaction, txs *transactionsByPriceAndNonce) (skip bool) {
	totalTxsSize := h.size + tx.Size()
	if totalTxsSize <= targetTxsSize {
		return false
	}
	// Abort transaction if it won't fit in the block and continue to search for a smaller
	// transction that will fit.
	log.Trace("Skipping transaction that would exceed target size", "hash", tx.Hash(), "totalTxsSize", totalTxsSize, "txSize", tx.Size())
	txs.Pop()
	return true
}

func (h *environmentHooks) makeFinalizeAndAssembleExtraArgs() []any {
	return []any{
		h.parent,
	}
}

type generateParamsHooks struct {
	predicateContext *precompileconfig.PredicateContext
}
