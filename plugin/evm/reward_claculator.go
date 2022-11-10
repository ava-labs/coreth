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
	"errors"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

const (
	percentDenominator uint64 = 1_000_000
	x2cRateUint64             = uint64(x2cRateInt64)
)

var (
	Slot0 = common.Hash{0x00}
	Slot1 = common.Hash{0x01}
	Slot2 = common.Hash{0x02}

	errInvalidRewardCalculation        = errors.New("invalid reward calculation")
	errRewardAndAmountToExportMismatch = errors.New("calculated reward differs with amount to export")
)

type RewardCalculation struct {
	ValidatorRewardAmount     *big.Int
	ValidatorRewardToExport   uint64
	IncentivePoolRewardAmount *big.Int
	CoinbaseAmountToSub       *big.Int

	// Needed for validation that calculation can be applied
	PrevFeesBurned           *big.Int
	PrevValidatorRewards     *big.Int
	PrevIncentivePoolRewards *big.Int
}

type RewardCalculationResult struct {
	ValidatorRewardAmount     common.Hash `serialize:"true" json:"validatorRewardAmount"`
	ValidatorRewardToExport   uint64      `serialize:"true" json:"validatorRewardToExport"`
	IncentivePoolRewardAmount common.Hash `serialize:"true" json:"incentivePoolRewardAmount"`
	CoinbaseAmountToSub       common.Hash `serialize:"true" json:"coinbaseAmountToSub"`

	// Needed for validation that calculation can be applied
	PrevFeesBurned           common.Hash `serialize:"true" json:"prevFeesBurned"`
	PrevValidatorRewards     common.Hash `serialize:"true" json:"prevValidatorRewards"`
	PrevIncentivePoolRewards common.Hash `serialize:"true" json:"prevIncentivePoolRewards"`
}

func (rc RewardCalculation) Result() RewardCalculationResult {
	return RewardCalculationResult{
		ValidatorRewardAmount:     common.BigToHash(rc.ValidatorRewardAmount),
		ValidatorRewardToExport:   rc.ValidatorRewardToExport,
		IncentivePoolRewardAmount: common.BigToHash(rc.IncentivePoolRewardAmount),
		CoinbaseAmountToSub:       common.BigToHash(rc.CoinbaseAmountToSub),
		PrevFeesBurned:            common.BigToHash(rc.PrevFeesBurned),
		PrevValidatorRewards:      common.BigToHash(rc.PrevValidatorRewards),
		PrevIncentivePoolRewards:  common.BigToHash(rc.PrevIncentivePoolRewards),
	}
}

func (rc RewardCalculation) Verify() error {
	if rc.ValidatorRewardToExport == 0 ||
		rc.ValidatorRewardAmount.Cmp(common.Big0) == 0 ||
		rc.IncentivePoolRewardAmount.Cmp(common.Big0) == 0 {
		return errInvalidRewardCalculation
	}

	exportAmount := bigDiv(rc.ValidatorRewardAmount, x2cRateUint64).Uint64()
	if exportAmount != rc.ValidatorRewardToExport {
		return errRewardAndAmountToExportMismatch
	}

	return nil
}

func (res RewardCalculationResult) Calculation() RewardCalculation {
	return RewardCalculation{
		ValidatorRewardAmount:     res.ValidatorRewardAmount.Big(),
		ValidatorRewardToExport:   res.ValidatorRewardToExport,
		IncentivePoolRewardAmount: res.IncentivePoolRewardAmount.Big(),
		CoinbaseAmountToSub:       res.CoinbaseAmountToSub.Big(),
		PrevFeesBurned:            res.PrevFeesBurned.Big(),
		PrevValidatorRewards:      res.PrevValidatorRewards.Big(),
		PrevIncentivePoolRewards:  res.PrevIncentivePoolRewards.Big(),
	}
}

func (res RewardCalculationResult) Verify() error {
	return res.Calculation().Verify()
}

// CalculateRewards calculates the rewards for validators and incentive pool account
//
//	feesBurned: the amount of fees burned already, balance of the coinbase address
//	validatorRewards: the amount of validator's rewards already paid out, state at slot 0 of coinbase
//	incentivePoolRewards: the amount of incentive pool's rewards already paid out, state at slot 0 of
//		incentive pool account
//	feeRewardRate: the percentage of fees to be paid out to validators, denominated in `percentDenominator`
//	incentivePoolRate: the percentage of fees to be paid out to incentive pool, denominated in `percentDenominator`
func CalculateRewards(
	feesBurned, validatorRewards, incentivePoolRewards *big.Int,
	feeRewardRate, incentivePoolRate uint64,
) (RewardCalculation, error) {
	calc := RewardCalculation{
		PrevFeesBurned:           feesBurned,
		PrevValidatorRewards:     validatorRewards,
		PrevIncentivePoolRewards: incentivePoolRewards,
	}
	errs := wrappers.Errs{}
	bigZero := big.NewInt(0)

	errs.Add(
		errIf(feeRewardRate+incentivePoolRate > percentDenominator, errors.New("feeRewardRate + incentivePoolRate > 100%")),
		errIf(feesBurned.Cmp(bigZero) < 0, errors.New("feesBurned < 0")),
		errIf(validatorRewards.Cmp(bigZero) < 0, errors.New("validatorRewards < 0")),
		errIf(incentivePoolRewards.Cmp(bigZero) < 0, errors.New("incentivePoolRewards < 0")),
	)
	if errs.Errored() {
		return calc, errs.Err
	}

	totalFeesAmount := big.NewInt(0).Add(feesBurned, incentivePoolRewards)
	totalFeesAmount.Add(totalFeesAmount, validatorRewards)

	feeRewardAmount := calculateReward(totalFeesAmount, validatorRewards, feeRewardRate)

	// Validator's reward is exported to the P-Chain, we intentionally loose precision so the C-Chain's
	// "decimal loss" can be accumulated in the account for future collections
	feeRewardAmount = bigDiv(feeRewardAmount, x2cRateUint64)
	calc.ValidatorRewardToExport = feeRewardAmount.Uint64()
	calc.ValidatorRewardAmount = bigMul(feeRewardAmount, x2cRateUint64)

	calc.IncentivePoolRewardAmount = calculateReward(totalFeesAmount, incentivePoolRewards, incentivePoolRate)

	calc.CoinbaseAmountToSub = new(big.Int).Add(calc.ValidatorRewardAmount, calc.IncentivePoolRewardAmount)

	return calc, calc.Verify()
}

func calculateReward(total, alreadyPayed *big.Int, percentRate uint64) *big.Int {
	reward := new(big.Int).Sub(
		bigMul(total, percentRate),
		bigMul(alreadyPayed, percentDenominator),
	)

	// denominate down from percentage
	return bigDiv(reward, percentDenominator)
}

// bigMul: multiply value by rate
func bigMul(value *big.Int, rate uint64) *big.Int {
	return new(big.Int).Mul(value, big.NewInt(int64(rate)))
}

// bigDiv: divide value by rate
func bigDiv(value *big.Int, rate uint64) *big.Int {
	return new(big.Int).Div(value, big.NewInt(int64(rate)))
}

func errIf(cond bool, err error) error {
	if cond {
		return err
	}
	return nil
}
