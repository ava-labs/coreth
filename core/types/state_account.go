// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"io"

	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

type (
	// Import these types from the go-ethereum package
	StateAccount = ethtypes.StateAccount
	SlimAccount  = ethtypes.SlimAccount
)

var (
	// Import these functions from the go-ethereum package
	NewEmptyStateAccount = ethtypes.NewEmptyStateAccount
	SlimAccountRLP       = ethtypes.SlimAccountRLP
	FullAccount          = ethtypes.FullAccount
	FullAccountRLP       = ethtypes.FullAccountRLP
)

type HeaderHooks Header_

func updateFromEth(h *Header_, eth *ethtypes.Header) {
	h.ParentHash = eth.ParentHash
	h.UncleHash = eth.UncleHash
	h.Coinbase = eth.Coinbase
	h.Root = eth.Root
	h.TxHash = eth.TxHash
	h.ReceiptHash = eth.ReceiptHash
	h.Bloom = eth.Bloom
	h.Difficulty = eth.Difficulty
	h.Number = eth.Number
	h.GasLimit = eth.GasLimit
	h.GasUsed = eth.GasUsed
	h.Time = eth.Time
	h.Extra = eth.Extra
	h.MixDigest = eth.MixDigest
	h.Nonce = eth.Nonce
	h.BaseFee = eth.BaseFee
	h.BlobGasUsed = eth.BlobGasUsed
	h.ExcessBlobGas = eth.ExcessBlobGas
	h.ParentBeaconRoot = eth.ParentBeaconRoot
}

func updateToEth(h *Header_, eth *ethtypes.Header) {
	eth.ParentHash = h.ParentHash
	eth.UncleHash = h.UncleHash
	eth.Coinbase = h.Coinbase
	eth.Root = h.Root
	eth.TxHash = h.TxHash
	eth.ReceiptHash = h.ReceiptHash
	eth.Bloom = h.Bloom
	eth.Difficulty = h.Difficulty
	eth.Number = h.Number
	eth.GasLimit = h.GasLimit
	eth.GasUsed = h.GasUsed
	eth.Time = h.Time
	eth.Extra = h.Extra
	eth.MixDigest = h.MixDigest
	eth.Nonce = h.Nonce
	eth.BaseFee = h.BaseFee
	eth.BlobGasUsed = h.BlobGasUsed
	eth.ExcessBlobGas = h.ExcessBlobGas
	eth.ParentBeaconRoot = h.ParentBeaconRoot
}

func (hh *HeaderHooks) EncodeRLP(eth *ethtypes.Header, writer io.Writer) error {
	// Write-back eth fields to the custom header
	h := HeaderExtras(eth)
	updateFromEth(h, eth)

	// Encode the custom header
	return h.EncodeRLP(writer)
}

func (hh *HeaderHooks) DecodeRLP(eth *ethtypes.Header, stream *rlp.Stream) error {
	// First, decode into the custom header
	h := HeaderExtras(eth)
	if err := stream.Decode(h); err != nil {
		return err
	}

	// Then, write-back the custom header fields to the eth header
	updateToEth(h, eth)
	return nil
}

//nolint:stdmethods
func (hh *HeaderHooks) MarshalJSON(eth *ethtypes.Header) ([]byte, error) {
	// Write-back eth fields to the custom header
	h := HeaderExtras(eth)
	updateFromEth(h, eth)

	// Marshal the custom header
	return h.MarshalJSON()
}

//nolint:stdmethods
func (hh *HeaderHooks) UnmarshalJSON(eth *ethtypes.Header, input []byte) error {
	// First, unmarshal into the custom header
	h := HeaderExtras(eth)
	if err := h.UnmarshalJSON(input); err != nil {
		return err
	}

	// Then, write-back the custom header fields to the eth header
	updateToEth(h, eth)
	return nil
}

type isMultiCoin bool

var (
	extras              = ethtypes.RegisterExtras[HeaderHooks, *HeaderHooks, isMultiCoin]()
	IsMultiCoinPayloads = extras.StateAccount
)

func IsMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return bool(extras.StateAccount.Get(s))
}

func HeaderExtras(h *Header) *Header_ {
	return (*Header_)(extras.Header.Get(h))
}

func WithHeaderExtras(h *Header, extra *Header_) *Header {
	extras.Header.Set(h, (*HeaderHooks)(extra))
	return h
}
