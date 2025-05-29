// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"

	customheader "github.com/ava-labs/coreth/plugin/evm/header"
)

var (
	ErrInsufficientBlockGas = errors.New("insufficient gas to cover the block cost")

	errUnclesUnsupported = errors.New("uncles unsupported")
)

type Mode struct {
	ModeSkipHeader   bool
	ModeSkipBlockFee bool
	ModeSkipCoinbase bool
}

type (
	OnFinalizeAndAssembleCallbackType = func(
		header *types.Header,
		parent *types.Header,
		state *state.StateDB,
		txs []*types.Transaction,
	) (
		extraData []byte,
		blockFeeContribution *big.Int,
		extDataGasUsed uint64,
		err error,
	)

	OnExtraStateChangeType = func(
		block *types.Block,
		parent *types.Header,
		statedb *state.StateDB,
	) (
		blockFeeContribution *big.Int,
		err error,
	)

	ConsensusCallbacks struct {
		OnFinalizeAndAssemble OnFinalizeAndAssembleCallbackType
		OnExtraStateChange    OnExtraStateChangeType
	}

	DummyEngine struct {
		cb                  ConsensusCallbacks
		clock               *mockable.Clock
		consensusMode       Mode
		desiredTargetExcess *gas.Gas
	}
)

func NewDummyEngine(
	cb ConsensusCallbacks,
	mode Mode,
	clock *mockable.Clock,
	desiredTargetExcess *gas.Gas,
) *DummyEngine {
	return &DummyEngine{
		cb:                  cb,
		clock:               clock,
		consensusMode:       mode,
		desiredTargetExcess: desiredTargetExcess,
	}
}

func NewETHFaker() *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: Mode{ModeSkipBlockFee: true},
	}
}

func NewFaker() *DummyEngine {
	return &DummyEngine{
		clock: &mockable.Clock{},
	}
}

func NewFakerWithClock(cb ConsensusCallbacks, clock *mockable.Clock) *DummyEngine {
	return &DummyEngine{
		cb:    cb,
		clock: clock,
	}
}

func NewFakerWithCallbacks(cb ConsensusCallbacks) *DummyEngine {
	return &DummyEngine{
		cb:    cb,
		clock: &mockable.Clock{},
	}
}

func NewFakerWithMode(cb ConsensusCallbacks, mode Mode) *DummyEngine {
	return &DummyEngine{
		cb:            cb,
		clock:         &mockable.Clock{},
		consensusMode: mode,
	}
}

func NewFakerWithModeAndClock(mode Mode, clock *mockable.Clock) *DummyEngine {
	return &DummyEngine{
		clock:         clock,
		consensusMode: mode,
	}
}

func NewCoinbaseFaker() *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: Mode{ModeSkipCoinbase: true},
	}
}

func NewFullFaker() *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: Mode{ModeSkipHeader: true},
	}
}

func (*DummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (eng *DummyEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (*DummyEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	return nil
}

func (*DummyEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	header.Difficulty = big.NewInt(1)
	return nil
}

func (eng *DummyEngine) verifyBlockFee(
	baseFee *big.Int,
	requiredBlockGasCost *big.Int,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	extraStateChangeContribution *big.Int,
) error {
	if eng.consensusMode.ModeSkipBlockFee {
		return nil
	}
	if baseFee == nil || baseFee.Sign() <= 0 {
		return fmt.Errorf("invalid base fee (%d) in apricot phase 4", baseFee)
	}
	if requiredBlockGasCost == nil || !requiredBlockGasCost.IsUint64() {
		return fmt.Errorf("invalid block gas cost (%d) in apricot phase 4", requiredBlockGasCost)
	}

	var (
		gasUsed              = new(big.Int)
		blockFeeContribution = new(big.Int)
		totalBlockFee        = new(big.Int)
	)

	// Add in the external contribution
	if extraStateChangeContribution != nil {
		if extraStateChangeContribution.Cmp(common.Big0) < 0 {
			return fmt.Errorf("invalid extra state change contribution: %d", extraStateChangeContribution)
		}
		totalBlockFee.Add(totalBlockFee, extraStateChangeContribution)
	}

	// Calculate the total excess (denominated in AVAX) over the base fee that was paid towards the block fee
	for i, receipt := range receipts {
		// Each transaction contributes the excess over the baseFee towards the totalBlockFee
		// This should be equivalent to the sum of the "priority fees" within EIP-1559.
		txFeePremium, err := txs[i].EffectiveGasTip(baseFee)
		if err != nil {
			return err
		}
		// Multiply the [txFeePremium] by the gasUsed in the transaction since this gives the total AVAX that was paid
		// above the amount required if the transaction had simply paid the minimum base fee for the block.
		//
		// Ex. LegacyTx paying a gas price of 100 gwei for 1M gas in a block with a base fee of 10 gwei.
		// Total Fee = 100 gwei * 1M gas
		// Minimum Fee = 10 gwei * 1M gas (minimum fee that would have been accepted for this transaction)
		// Fee Premium = 90 gwei
		// Total Overpaid = 90 gwei * 1M gas

		blockFeeContribution.Mul(txFeePremium, gasUsed.SetUint64(receipt.GasUsed))
		totalBlockFee.Add(totalBlockFee, blockFeeContribution)
	}
	// Calculate how much gas the [totalBlockFee] would purchase at the price level
	// set by the base fee of this block.
	blockGas := new(big.Int).Div(totalBlockFee, baseFee)

	// Require that the amount of gas purchased by the effective tips within the
	// block covers at least `requiredBlockGasCost`.
	//
	// NOTE: To determine the required block fee, multiply
	// `requiredBlockGasCost` by `baseFee`.
	if blockGas.Cmp(requiredBlockGasCost) < 0 {
		return fmt.Errorf("%w: expected %d but got %d",
			ErrInsufficientBlockGas,
			requiredBlockGasCost,
			blockGas,
		)
	}
	return nil
}

func (eng *DummyEngine) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, state *state.StateDB, receipts []*types.Receipt) error {
	// Perform extra state change while finalizing the block
	var (
		contribution *big.Int
		err          error
	)
	if eng.cb.OnExtraStateChange != nil {
		contribution, err = eng.cb.OnExtraStateChange(block, parent, state)
		if err != nil {
			return err
		}
	}

	config := params.GetExtra(chain.Config())
	timestamp := block.Time()
	if config.IsApricotPhase4(timestamp) {
		// Verify the block fee was paid.
		blockGasCost := customtypes.BlockGasCost(block)
		if err := eng.verifyBlockFee(
			block.BaseFee(),
			blockGasCost,
			block.Transactions(),
			receipts,
			contribution,
		); err != nil {
			return err
		}
	}

	return nil
}

func (eng *DummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt,
) (*types.Block, error) {
	var (
		contribution   *big.Int
		extDataGasUsed uint64
		extraData      []byte
		err            error
	)
	if eng.cb.OnFinalizeAndAssemble != nil {
		extraData, contribution, extDataGasUsed, err = eng.cb.OnFinalizeAndAssemble(header, parent, state, txs)
		if err != nil {
			return nil, err
		}
	}

	configExtra := params.GetExtra(chain.Config())
	headerExtra := customtypes.GetHeaderExtra(header)
	// Calculate the required block gas cost for this block.
	headerExtra.BlockGasCost = customheader.BlockGasCost(
		configExtra,
		parent,
		header.Time,
	)
	if configExtra.IsApricotPhase4(header.Time) {
		headerExtra.ExtDataGasUsed = new(big.Int).SetUint64(extDataGasUsed)

		// Verify that this block covers the block fee.
		if err := eng.verifyBlockFee(
			header.BaseFee,
			headerExtra.BlockGasCost,
			txs,
			receipts,
			contribution,
		); err != nil {
			return nil, err
		}
	}

	// finalize the header.Extra
	extraPrefix, err := customheader.ExtraPrefix(configExtra, parent, header, eng.desiredTargetExcess)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate new header.Extra: %w", err)
	}
	header.Extra = append(extraPrefix, header.Extra...)

	// commit the final state root
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return customtypes.NewBlockWithExtData(
		header, txs, uncles, receipts, trie.NewStackTrie(nil),
		extraData, configExtra.IsApricotPhase1(header.Time),
	), nil
}

func (*DummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (*DummyEngine) Close() error {
	return nil
}
