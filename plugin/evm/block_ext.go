// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ethereum/go-ethereum/log"
)

// calculateAndCollectRewards calculates the rewards and issues the CollectRewardsTx
// Errors are logged and ignored as they are not affecting the other actions
func (b *Block) calculateAndCollectRewards() {
	state, err := b.vm.blockChain.State()
	if err == nil {
		calc, err := b.calculateRewards(state)

		if err != nil {
			log.Info("Calculation of the rewards skipped", "error", err)
		}
		tx, err := b.createReawardsCollectionTx(calc)
		if err != nil {
			log.Info("Issuing of the rewards collection skipped", "error", err)
		} else {
			log.Info("Issuing of the rewards collection tx", "txID", tx.ID())
			b.vm.issueTx(tx, true /*=local*/)
		}
	}
}
func (b *Block) calculateRewards(state *state.StateDB) (*RewardCalculation, error) {
	header := b.ethBlock.Header()

	feesBurned := state.GetBalance(header.Coinbase)
	validatorRewards := state.GetState(header.Coinbase, Slot1).Big()
	incentivePoolRewards := state.GetState(header.Coinbase, Slot2).Big()

	calculation, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		header.FeeRewardRate,
		header.IncentivePoolRewardRate,
	)

	if err != nil {
		return nil, err
	}

	if calculation.ValidatorRewardToExport < header.FeeRewardMinAmountToExport {
		return nil, fmt.Errorf("calculated fee reward amount %d is less than the minimum amount to export", calculation.ValidatorRewardAmount)
	}

	return &calculation, nil
}

func (b *Block) createReawardsCollectionTx(calculation *RewardCalculation) (*Tx, error) {
	if calculation == nil {
		return nil, fmt.Errorf("no rewards to collect")
	}

	h := b.ethBlock.Header()
	tx, err := b.vm.NewCollectRewardsTx(
		calculation.Result(),
		b.ID(),
		b.ethBlock.Time(),
		h.Coinbase,
		h.FeeRewardExportAddress,
		h.IncentivePoolRewardAddress,
	)
	if err != nil {
		return nil, err
	}

	return tx, nil
}
