// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package abi

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: This file contains tests in addition to those found in go-ethereum.

const TEST_ABI = `[{"type":"function","name":"receive","inputs":[{"name":"sender","type":"address"},{"name":"amount","type":"uint256"},{"name":"memo","type":"bytes"}],"outputs":[{"internalType":"bool","name":"isAllowed","type":"bool"}]}]`

func TestUnpackInputIntoInterface(t *testing.T) {
	abi, err := JSON(strings.NewReader(TEST_ABI))
	require.NoError(t, err)

	type inputType struct {
		Sender common.Address
		Amount *big.Int
		Memo   []byte
	}
	input := inputType{
		Sender: common.HexToAddress("0x02"),
		Amount: big.NewInt(100),
		Memo:   []byte("hello"),
	}

	rawData, err := abi.Pack("receive", input.Sender, input.Amount, input.Memo)
	require.NoError(t, err)

	abi, err = JSON(strings.NewReader(TEST_ABI))
	require.NoError(t, err)

	for _, test := range []struct {
		name                   string
		extraPaddingBytes      int
		strictMode             bool
		expectedErrorSubstring string
	}{
		{
			name:       "No extra padding to input data",
			strictMode: true,
		},
		{
			name:              "Valid input data with 32 extra padding(%32) ",
			extraPaddingBytes: 32,
			strictMode:        true,
		},
		{
			name:              "Valid input data with 64 extra padding(%32)",
			extraPaddingBytes: 64,
			strictMode:        true,
		},
		{
			name:                   "Valid input data with extra padding indivisible by 32",
			extraPaddingBytes:      33,
			strictMode:             true,
			expectedErrorSubstring: "abi: improperly formatted input:",
		},
		{
			name:              "Valid input data with extra padding indivisible by 32, no strict mode",
			extraPaddingBytes: 33,
			strictMode:        false,
		},
	} {
		{
			t.Run(test.name, func(t *testing.T) {
				// skip 4 byte selector
				data := rawData[4:]
				// Add extra padding to data
				data = append(data, make([]byte, test.extraPaddingBytes)...)

				// Unpack into interface
				var v inputType
				err = abi.UnpackInputIntoInterface(&v, "receive", data, test.strictMode) // skips 4 byte selector

				if test.expectedErrorSubstring != "" {
					require.Error(t, err)
					require.ErrorContains(t, err, test.expectedErrorSubstring)
				} else {
					require.NoError(t, err)
					// Verify unpacked values match input
					require.Equal(t, v.Amount, input.Amount)
					require.EqualValues(t, v.Amount, input.Amount)
					require.True(t, bytes.Equal(v.Memo, input.Memo))
				}
			})
		}
	}
}

func TestPackOutput(t *testing.T) {
	abi, err := JSON(strings.NewReader(TEST_ABI))
	require.NoError(t, err)

	bytes, err := abi.PackOutput("receive", true)
	require.NoError(t, err)

	vals, err := abi.Methods["receive"].Outputs.Unpack(bytes)
	require.NoError(t, err)

	require.Len(t, vals, 1)
	require.True(t, vals[0].(bool))
}

func TestABI_PackEvent(t *testing.T) {
	tests := []struct {
		name           string
		json           string
		event          string
		args           []interface{}
		expectedTopics []common.Hash
		expectedData   []byte
	}{
		{
			name: "received",
			json: `[
			{"type":"event","name":"received","anonymous":false,"inputs":[
				{"indexed":false,"name":"sender","type":"address"},
				{"indexed":false,"name":"amount","type":"uint256"},
				{"indexed":false,"name":"memo","type":"bytes"}
				]
			}]`,
			event: "received(address,uint256,bytes)",
			args: []interface{}{
				common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
				big.NewInt(1),
				[]byte{0x88},
			},
			expectedTopics: []common.Hash{
				common.HexToHash("0x75fd880d39c1daf53b6547ab6cb59451fc6452d27caa90e5b6649dd8293b9eed"),
			},
			expectedData: common.Hex2Bytes("000000000000000000000000376c47978271565f56deb45495afa69e59c16ab20000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000018800000000000000000000000000000000000000000000000000000000000000"),
		},
		{
			name: "received",
			json: `[
			{"type":"event","name":"received","anonymous":true,"inputs":[
				{"indexed":false,"name":"sender","type":"address"},
				{"indexed":false,"name":"amount","type":"uint256"},
				{"indexed":false,"name":"memo","type":"bytes"}
				]
			}]`,
			event: "received(address,uint256,bytes)",
			args: []interface{}{
				common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
				big.NewInt(1),
				[]byte{0x88},
			},
			expectedTopics: []common.Hash{},
			expectedData:   common.Hex2Bytes("000000000000000000000000376c47978271565f56deb45495afa69e59c16ab20000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000018800000000000000000000000000000000000000000000000000000000000000"),
		}, {
			name: "Transfer",
			json: `[
				{ "constant": true, "inputs": [], "name": "name", "outputs": [ { "name": "", "type": "string" } ], "payable": false, "stateMutability": "view", "type": "function" },
				{ "constant": false, "inputs": [ { "name": "_spender", "type": "address" }, { "name": "_value", "type": "uint256" } ], "name": "approve", "outputs": [ { "name": "", "type": "bool" } ], "payable": false, "stateMutability": "nonpayable", "type": "function" },
				{ "constant": true, "inputs": [], "name": "totalSupply", "outputs": [ { "name": "", "type": "uint256" } ], "payable": false, "stateMutability": "view", "type": "function" },
				{ "constant": false, "inputs": [ { "name": "_from", "type": "address" }, { "name": "_to", "type": "address" }, { "name": "_value", "type": "uint256" } ], "name": "transferFrom", "outputs": [ { "name": "", "type": "bool" } ], "payable": false, "stateMutability": "nonpayable", "type": "function" },
				{ "constant": true, "inputs": [], "name": "decimals", "outputs": [ { "name": "", "type": "uint8" } ], "payable": false, "stateMutability": "view", "type": "function" },
				{ "constant": true, "inputs": [ { "name": "_owner", "type": "address" } ], "name": "balanceOf", "outputs": [ { "name": "balance", "type": "uint256" } ], "payable": false, "stateMutability": "view", "type": "function" },
				{ "constant": true, "inputs": [], "name": "symbol", "outputs": [ { "name": "", "type": "string" } ], "payable": false, "stateMutability": "view", "type": "function" },
				{ "constant": false, "inputs": [ { "name": "_to", "type": "address" }, { "name": "_value", "type": "uint256" } ], "name": "transfer", "outputs": [ { "name": "", "type": "bool" } ], "payable": false, "stateMutability": "nonpayable", "type": "function" },
				{ "constant": true, "inputs": [ { "name": "_owner", "type": "address" }, { "name": "_spender", "type": "address" } ], "name": "allowance", "outputs": [ { "name": "", "type": "uint256" } ], "payable": false, "stateMutability": "view", "type": "function" },
				{ "payable": true, "stateMutability": "payable", "type": "fallback" },
				{ "anonymous": false, "inputs": [ { "indexed": true, "name": "owner", "type": "address" }, { "indexed": true, "name": "spender", "type": "address" }, { "indexed": false, "name": "value", "type": "uint256" } ], "name": "Approval", "type": "event" },
				{ "anonymous": false, "inputs": [ { "indexed": true, "name": "from", "type": "address" }, { "indexed": true, "name": "to", "type": "address" }, { "indexed": false, "name": "value", "type": "uint256" } ], "name": "Transfer", "type": "event" }
			]`,
			event: "Transfer(address,address,uint256)",
			args: []interface{}{
				common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
				big.NewInt(100),
			},
			expectedTopics: []common.Hash{
				common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
				common.HexToHash("0x0000000000000000000000008db97c7cece249c2b98bdc0226cc4c2a57bf52fc"),
				common.HexToHash("0x000000000000000000000000376c47978271565f56deb45495afa69e59c16ab2"),
			},
			expectedData: common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000064"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			abi, err := JSON(strings.NewReader(test.json))
			if err != nil {
				t.Error(err)
			}

			topics, data, err := abi.PackEvent(test.name, test.args...)
			if err != nil {
				t.Fatal(err)
			}

			assert.EqualValues(t, test.expectedTopics, topics)
			assert.EqualValues(t, test.expectedData, data)
		})
	}
}
