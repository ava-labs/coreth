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
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	ZERO = big.NewInt(0)
	CAM1 = big.NewInt(1_000_000_000_000_000_000)
)

func TestRewardCalculationRatesExceed100Percent(t *testing.T) {
	blackHoleAddressBalance := big.NewInt(1_000_000_000_000_000_000)
	payedOutBalance := big.NewInt(0)
	incentivePoolBalance := big.NewInt(0)
	feeRewardRate := uint64(0.51 * float64(percentDenominator))
	incentivePoolRate := uint64(0.50 * float64(percentDenominator))

	_, err := CalculateRewards(
		blackHoleAddressBalance,
		payedOutBalance,
		incentivePoolBalance,
		feeRewardRate,
		incentivePoolRate,
	)

	assert.Error(t, err)
	assert.Equal(t, "feeRewardRate + incentivePoolRate > 100%", err.Error())
}

func TestRewardCalculationCalculate10PercentReward(t *testing.T) {
	blackHoleAddressBalance := big.NewInt(1_000_000_000_000_000_000)
	payedOutBalance := big.NewInt(0)
	incentivePoolBalance := big.NewInt(0)
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	calc, err := CalculateRewards(
		blackHoleAddressBalance,
		payedOutBalance,
		incentivePoolBalance,
		feeRewardRate,
		incentivePoolRate,
	)

	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(100_000_000_000_000_000), calc.ValidatorRewardAmount)
	assert.Equal(t, uint64(100_000_000), calc.ValidatorRewardToExport)
	assert.Equal(t, big.NewInt(100_000_000_000_000_000), calc.IncentivePoolRewardAmount)
	assert.Equal(t, big.NewInt(200_000_000_000_000_000), calc.CoinbaseAmountToSub)
}

func TestRewardCalculationCalculationWithoutFeeIncrementIsZeroRewards(t *testing.T) {
	feesBurned := CAM1
	validatorRewards := ZERO
	incentivePoolRewards := ZERO
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	calc1, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc1 := big.NewInt(0).Sub(feesBurned, calc1.CoinbaseAmountToSub)
	validatorAmountAfterCalc1 := big.NewInt(0).Add(validatorRewards, calc1.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc1 := big.NewInt(0).Add(incentivePoolRewards, calc1.IncentivePoolRewardAmount)

	calc2, err := CalculateRewards(
		coinbaseAmountAfterCalc1,
		validatorAmountAfterCalc1,
		incentivePoolRewardAmountAfterCalc1,
		feeRewardRate,
		incentivePoolRate,
	)
	// error is raised to prevent issuing zero rewards Tx
	assert.Error(t, err)
	assert.Equal(t, "invalid reward calculation", err.Error())

	assert.Equal(t, ZERO, calc2.CoinbaseAmountToSub)
	assert.Equal(t, ZERO, calc2.ValidatorRewardAmount)
	assert.Equal(t, ZERO, calc2.IncentivePoolRewardAmount)
}

func TestRewardCalculationSumOfCalculationsEqualsTotalCalculation(t *testing.T) {
	feesBurned := CAM1
	validatorRewards := ZERO
	incentivePoolRewards := ZERO
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	calc1, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc1 := big.NewInt(0).Sub(feesBurned, calc1.CoinbaseAmountToSub)
	validatorAmountAfterCalc1 := big.NewInt(0).Add(validatorRewards, calc1.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc1 := big.NewInt(0).Add(incentivePoolRewards, calc1.IncentivePoolRewardAmount)

	total := CAM1
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc1, validatorAmountAfterCalc1, incentivePoolRewardAmountAfterCalc1),
	)
	feesBurned = bigAdd(coinbaseAmountAfterCalc1, CAM1)
	calc2, err := CalculateRewards(
		feesBurned,
		validatorAmountAfterCalc1,
		incentivePoolRewardAmountAfterCalc1,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc2 := big.NewInt(0).Sub(feesBurned, calc2.CoinbaseAmountToSub)
	validatorAmountAfterCalc2 := bigAdd(validatorAmountAfterCalc1, calc2.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc2 := bigAdd(incentivePoolRewardAmountAfterCalc1, calc2.IncentivePoolRewardAmount)

	total = bigMul(CAM1, 2)
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc2, validatorAmountAfterCalc2, incentivePoolRewardAmountAfterCalc2),
	)

	feesBurned = bigAdd(coinbaseAmountAfterCalc2, CAM1)
	calc3, err := CalculateRewards(
		feesBurned,
		validatorAmountAfterCalc2,
		incentivePoolRewardAmountAfterCalc2,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc3 := big.NewInt(0).Sub(feesBurned, calc3.CoinbaseAmountToSub)
	validatorAmountAfterCalc3 := bigAdd(validatorAmountAfterCalc2, calc3.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc3 := bigAdd(incentivePoolRewardAmountAfterCalc2, calc3.IncentivePoolRewardAmount)

	total = bigMul(CAM1, 3)
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc3, validatorAmountAfterCalc3, incentivePoolRewardAmountAfterCalc3),
	)

	calcTotal, err := CalculateRewards(
		bigMul(CAM1, 3),
		ZERO,
		ZERO,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	assert.Equal(t, incentivePoolRewardAmountAfterCalc3, validatorAmountAfterCalc3)
	assert.Equal(t, calcTotal.ValidatorRewardAmount, validatorAmountAfterCalc3)
	assert.Equal(t, calcTotal.IncentivePoolRewardAmount, incentivePoolRewardAmountAfterCalc3)
}

func TestRewardCalculationRaiseRateAfterFirstCalculation(t *testing.T) {
	feesBurned := CAM1
	validatorRewards := ZERO
	incentivePoolRewards := ZERO
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	calc1, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc1 := big.NewInt(0).Sub(feesBurned, calc1.CoinbaseAmountToSub)
	validatorAmountAfterCalc1 := big.NewInt(0).Add(validatorRewards, calc1.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc1 := big.NewInt(0).Add(incentivePoolRewards, calc1.IncentivePoolRewardAmount)

	total := CAM1
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc1, validatorAmountAfterCalc1, incentivePoolRewardAmountAfterCalc1),
	)
	feesBurned = bigAdd(coinbaseAmountAfterCalc1, CAM1)
	feeRewardRate = uint64(0.20 * float64(percentDenominator))
	incentivePoolRate = uint64(0.20 * float64(percentDenominator))
	calc2, err := CalculateRewards(
		feesBurned,
		validatorAmountAfterCalc1,
		incentivePoolRewardAmountAfterCalc1,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc2 := big.NewInt(0).Sub(feesBurned, calc2.CoinbaseAmountToSub)
	validatorAmountAfterCalc2 := bigAdd(validatorAmountAfterCalc1, calc2.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc2 := bigAdd(incentivePoolRewardAmountAfterCalc1, calc2.IncentivePoolRewardAmount)

	total = bigMul(CAM1, 2)
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc2, validatorAmountAfterCalc2, incentivePoolRewardAmountAfterCalc2),
	)

	feesBurned = bigAdd(coinbaseAmountAfterCalc2, CAM1)

	calcTotal, err := CalculateRewards(
		bigMul(CAM1, 2),
		ZERO,
		ZERO,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	assert.Equal(t, incentivePoolRewardAmountAfterCalc2, validatorAmountAfterCalc2)
	assert.Equal(t, calcTotal.ValidatorRewardAmount, validatorAmountAfterCalc2)
	assert.Equal(t, calcTotal.IncentivePoolRewardAmount, incentivePoolRewardAmountAfterCalc2)
}

func TestRewardCalculationLowerRateAfterFirstCalculation(t *testing.T) {
	feesBurned := CAM1
	validatorRewards := ZERO
	incentivePoolRewards := ZERO
	feeRewardRate := uint64(0.20 * float64(percentDenominator))
	incentivePoolRate := uint64(0.20 * float64(percentDenominator))

	calc1, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc1 := big.NewInt(0).Sub(feesBurned, calc1.CoinbaseAmountToSub)
	validatorAmountAfterCalc1 := big.NewInt(0).Add(validatorRewards, calc1.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc1 := big.NewInt(0).Add(incentivePoolRewards, calc1.IncentivePoolRewardAmount)

	total := CAM1
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc1, validatorAmountAfterCalc1, incentivePoolRewardAmountAfterCalc1),
	)
	feesBurned = bigAdd(coinbaseAmountAfterCalc1, CAM1)
	feeRewardRate = uint64(0.10 * float64(percentDenominator))
	incentivePoolRate = uint64(0.10 * float64(percentDenominator))
	calc2, err := CalculateRewards(
		feesBurned,
		validatorAmountAfterCalc1,
		incentivePoolRewardAmountAfterCalc1,
		feeRewardRate,
		incentivePoolRate,
	)
	// error is raised to prevent issuing zero rewards Tx
	assert.Error(t, err)
	assert.Equal(t, "invalid reward calculation", err.Error())

	coinbaseAmountAfterCalc2 := big.NewInt(0).Sub(feesBurned, calc2.CoinbaseAmountToSub)
	validatorAmountAfterCalc2 := bigAdd(validatorAmountAfterCalc1, calc2.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc2 := bigAdd(incentivePoolRewardAmountAfterCalc1, calc2.IncentivePoolRewardAmount)

	total = bigMul(CAM1, 2)
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc2, validatorAmountAfterCalc2, incentivePoolRewardAmountAfterCalc2),
	)

	feesBurned = bigAdd(coinbaseAmountAfterCalc2, CAM1)

	calcTotal, err := CalculateRewards(
		bigMul(CAM1, 2),
		ZERO,
		ZERO,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	assert.Equal(t, incentivePoolRewardAmountAfterCalc2, validatorAmountAfterCalc2)
	assert.Equal(t, calcTotal.ValidatorRewardAmount, validatorAmountAfterCalc2)
	assert.Equal(t, calcTotal.IncentivePoolRewardAmount, incentivePoolRewardAmountAfterCalc2)
}

func TestRewardCalculationPercentageChangeAfterFirstCalculationAndBackBeforeTotalCalculation(t *testing.T) {
	feesBurned := CAM1
	validatorRewards := ZERO
	incentivePoolRewards := ZERO
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	calc1, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc1 := big.NewInt(0).Sub(feesBurned, calc1.CoinbaseAmountToSub)
	validatorAmountAfterCalc1 := big.NewInt(0).Add(validatorRewards, calc1.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc1 := big.NewInt(0).Add(incentivePoolRewards, calc1.IncentivePoolRewardAmount)

	total := CAM1
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc1, validatorAmountAfterCalc1, incentivePoolRewardAmountAfterCalc1),
	)
	feesBurned = bigAdd(coinbaseAmountAfterCalc1, CAM1)

	feeRewardRate = uint64(0.20 * float64(percentDenominator))
	incentivePoolRate = uint64(0.20 * float64(percentDenominator))
	calc2, err := CalculateRewards(
		feesBurned,
		validatorAmountAfterCalc1,
		incentivePoolRewardAmountAfterCalc1,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	coinbaseAmountAfterCalc2 := big.NewInt(0).Sub(feesBurned, calc2.CoinbaseAmountToSub)
	validatorAmountAfterCalc2 := bigAdd(validatorAmountAfterCalc1, calc2.ValidatorRewardAmount)
	incentivePoolRewardAmountAfterCalc2 := bigAdd(incentivePoolRewardAmountAfterCalc1, calc2.IncentivePoolRewardAmount)

	total = bigMul(CAM1, 2)
	assert.Equal(t,
		total,
		bigAdd(coinbaseAmountAfterCalc2, validatorAmountAfterCalc2, incentivePoolRewardAmountAfterCalc2),
	)

	feesBurned = bigAdd(coinbaseAmountAfterCalc2, CAM1)

	feeRewardRate = uint64(0.10 * float64(percentDenominator))
	incentivePoolRate = uint64(0.10 * float64(percentDenominator))

	calcTotal, err := CalculateRewards(
		bigMul(CAM1, 3),
		ZERO,
		ZERO,
		feeRewardRate,
		incentivePoolRate,
	)
	assert.NoError(t, err)

	/*
		The way calculation is done does not allow to change rates on the go, changing rates requires more work
		than just changing rate parameters (we need to freeze current state, calculate total on new rate, set all
		the states to the previous values
	*/
	assert.Equal(t, incentivePoolRewardAmountAfterCalc2, validatorAmountAfterCalc2)
	assert.NotEqual(t, calcTotal.ValidatorRewardAmount, validatorAmountAfterCalc2)
	assert.NotEqual(t, calcTotal.IncentivePoolRewardAmount, incentivePoolRewardAmountAfterCalc2)
}

func TestRewardCalculationNegativeBlackHoleAddressBalance(t *testing.T) {
	feesBurned := big.NewInt(-1)
	validatorRewards := big.NewInt(0)
	incentivePoolRewards := big.NewInt(0)
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	_, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)

	assert.Error(t, err)
	assert.Equal(t, "feesBurned < 0", err.Error())
}

func TestRewardCalculationNegativePayedOutBalance(t *testing.T) {
	feesBurned := big.NewInt(1_000_000_000_000_000_000)
	validatorRewards := big.NewInt(-1)
	incentivePoolRewards := big.NewInt(0)
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	_, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)

	assert.Error(t, err)
	assert.Equal(t, "validatorRewards < 0", err.Error())
}

func TestRewardCalculationNegativeIncentivePoolBalance(t *testing.T) {
	feesBurned := big.NewInt(1_000_000_000_000_000_000)
	validatorRewards := big.NewInt(0)
	incentivePoolRewards := big.NewInt(-1)
	feeRewardRate := uint64(0.10 * float64(percentDenominator))
	incentivePoolRate := uint64(0.10 * float64(percentDenominator))

	_, err := CalculateRewards(
		feesBurned,
		validatorRewards,
		incentivePoolRewards,
		feeRewardRate,
		incentivePoolRate,
	)

	assert.Error(t, err)
	assert.Equal(t, "incentivePoolRewards < 0", err.Error())
}

func bigAdd(ingredients ...*big.Int) *big.Int {
	sum := big.NewInt(0)
	for _, ingredient := range ingredients {
		sum.Add(sum, ingredient)
	}
	return sum
}
