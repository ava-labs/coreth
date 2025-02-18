// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap3"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/require"
)

type blockDefinition struct {
	timestamp      uint64
	gasUsed        uint64
	extDataGasUsed *big.Int
}

type test struct {
	extraData      []byte
	baseFee        *big.Int
	genBlocks      func() []blockDefinition
	minFee, maxFee *big.Int
}

func TestDynamicFees(t *testing.T) {
	spacedTimestamps := []uint64{1, 1, 2, 5, 15, 120}

	var tests []test = []test{
		// Test minimal gas usage
		{
			extraData: nil,
			baseFee:   nil,
			minFee:    big.NewInt(params.ApricotPhase3MinBaseFee),
			maxFee:    big.NewInt(params.ApricotPhase3MaxBaseFee),
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   21000,
					})
				}
				return blocks
			},
		},
		// Test overflow handling
		{
			extraData: nil,
			baseFee:   nil,
			minFee:    big.NewInt(params.ApricotPhase3MinBaseFee),
			maxFee:    big.NewInt(params.ApricotPhase3MaxBaseFee),
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   math.MaxUint64,
					})
				}
				return blocks
			},
		},
		{
			extraData: nil,
			baseFee:   nil,
			minFee:    big.NewInt(params.ApricotPhase3MinBaseFee),
			maxFee:    big.NewInt(params.ApricotPhase3MaxBaseFee),
			genBlocks: func() []blockDefinition {
				return []blockDefinition{
					{
						timestamp: 1,
						gasUsed:   1_000_000,
					},
					{
						timestamp: 3,
						gasUsed:   1_000_000,
					},
					{
						timestamp: 5,
						gasUsed:   2_000_000,
					},
					{
						timestamp: 5,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 7,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1000,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1001,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1002,
						gasUsed:   6_000_000,
					},
				}
			},
		},
	}

	for _, test := range tests {
		testDynamicFeesStaysWithinRange(t, test)
	}
}

func testDynamicFeesStaysWithinRange(t *testing.T, test test) {
	blocks := test.genBlocks()
	initialBlock := blocks[0]
	header := &types.Header{
		Time:    initialBlock.timestamp,
		GasUsed: initialBlock.gasUsed,
		Number:  big.NewInt(0),
		BaseFee: test.baseFee,
		Extra:   test.extraData,
	}

	for index, block := range blocks[1:] {
		nextExtraData, err := CalcExtraPrefix(params.TestApricotPhase3Config, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate extra prefix at index %d: %s", index, err)
		}
		nextBaseFee, err := CalcBaseFee(params.TestApricotPhase3Config, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate base fee at index %d: %s", index, err)
		}
		if nextBaseFee.Cmp(test.maxFee) > 0 {
			t.Fatalf("Expected fee to stay less than %d, but found %d", test.maxFee, nextBaseFee)
		}
		if nextBaseFee.Cmp(test.minFee) < 0 {
			t.Fatalf("Expected fee to stay greater than %d, but found %d", test.minFee, nextBaseFee)
		}
		log.Info("Update", "baseFee", nextBaseFee)
		header = &types.Header{
			Time:    block.timestamp,
			GasUsed: block.gasUsed,
			Number:  big.NewInt(int64(index) + 1),
			BaseFee: nextBaseFee,
			Extra:   nextExtraData,
		}
	}
}

func TestSelectBigWithinBounds(t *testing.T) {
	type test struct {
		lower, value, upper, expected *big.Int
	}

	tests := map[string]test{
		"value within bounds": {
			lower:    big.NewInt(0),
			value:    big.NewInt(5),
			upper:    big.NewInt(10),
			expected: big.NewInt(5),
		},
		"value below lower bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(-1),
			upper:    big.NewInt(10),
			expected: big.NewInt(0),
		},
		"value above upper bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(11),
			upper:    big.NewInt(10),
			expected: big.NewInt(10),
		},
		"value matches lower bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(0),
			upper:    big.NewInt(10),
			expected: big.NewInt(0),
		},
		"value matches upper bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(10),
			upper:    big.NewInt(10),
			expected: big.NewInt(10),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			v := selectBigWithinBounds(test.lower, test.value, test.upper)
			if v.Cmp(test.expected) != 0 {
				t.Fatalf("Expected (%d), found (%d)", test.expected, v)
			}
		})
	}
}

func TestParseDynamicFeeWindow(t *testing.T) {
	tests := []struct {
		name     string
		bytes    []byte
		window   ap3.Window
		parseErr error
	}{
		{
			name:     "insufficient_length",
			bytes:    make([]byte, DynamicFeeWindowSize-1),
			parseErr: errDynamicFeeWindowInsufficientLength,
		},
		{
			name:   "zero_window",
			bytes:  make([]byte, DynamicFeeWindowSize),
			window: ap3.Window{},
		},
		{
			name: "truncate_bytes",
			bytes: []byte{
				DynamicFeeWindowSize: 1,
			},
			window: ap3.Window{},
		},
		{
			name: "endianess",
			bytes: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
				0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
				0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
				0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
				0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
				0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
			},
			window: ap3.Window{
				0x0102030405060708,
				0x1112131415161718,
				0x2122232425262728,
				0x3132333435363738,
				0x4142434445464748,
				0x5152535455565758,
				0x6162636465666768,
				0x7172737475767778,
				0x8182838485868788,
				0x9192939495969798,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			window, err := parseDynamicFeeWindow(test.bytes)
			require.Equal(test.window, window)
			require.ErrorIs(err, test.parseErr)
			if test.parseErr != nil {
				return
			}

			expectedBytes := test.bytes[:DynamicFeeWindowSize]
			bytes := dynamicFeeWindowBytes(window)
			require.Equal(expectedBytes, bytes)
		})
	}
}
