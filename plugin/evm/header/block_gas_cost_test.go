// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/ap4"
	"github.com/stretchr/testify/assert"
)

func TestBlockGasCostWithStep(t *testing.T) {
	tests := []struct {
		name        string
		parentCost  *big.Int
		parentTime  uint64
		currentTime uint64
		expected    uint64
	}{
		{
			name:        "Nil parentBlockGasCost",
			parentCost:  nil,
			parentTime:  1,
			currentTime: 1,
			expected:    ap4.MinBlockGasCost,
		},
		{
			name:        "Same timestamp from 0",
			parentCost:  big.NewInt(0),
			parentTime:  1,
			currentTime: 1,
			expected:    ap4.TargetBlockRate * ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "1s from 0",
			parentCost:  big.NewInt(0),
			parentTime:  1,
			currentTime: 2,
			expected:    (ap4.TargetBlockRate - 1) * ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "Same timestamp from non-zero",
			parentCost:  big.NewInt(50_000),
			parentTime:  1,
			currentTime: 1,
			expected:    50_000 + ap4.TargetBlockRate*ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "0s Difference (MAX)",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 1,
			expected:    ap4.MaxBlockGasCost,
		},
		{
			name:        "1s Difference (MAX)",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 2,
			expected:    ap4.MaxBlockGasCost,
		},
		{
			name:        "2s Difference",
			parentCost:  big.NewInt(900_000),
			parentTime:  1,
			currentTime: 3,
			expected:    900_000 + (ap4.TargetBlockRate-2)*ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "3s Difference",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 4,
			expected:    ap4.MaxBlockGasCost + (ap4.TargetBlockRate-3)*ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "10s Difference",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 11,
			expected:    ap4.MaxBlockGasCost + (ap4.TargetBlockRate-10)*ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "20s Difference",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 21,
			expected:    ap4.MaxBlockGasCost + (ap4.TargetBlockRate-20)*ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "22s Difference",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 23,
			expected:    ap4.MaxBlockGasCost + (ap4.TargetBlockRate-22)*ApricotPhase4BlockGasCostStep,
		},
		{
			name:        "23s Difference",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			parentTime:  1,
			currentTime: 24,
			expected:    0,
		},
		{
			name:        "-1s Difference",
			parentCost:  big.NewInt(50_000),
			parentTime:  1,
			currentTime: 0,
			expected:    50_000 + ap4.TargetBlockRate*ApricotPhase4BlockGasCostStep,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, BlockGasCostWithStep(
				test.parentCost,
				ApricotPhase4BlockGasCostStep,
				test.parentTime,
				test.currentTime,
			))
		})
	}
}
