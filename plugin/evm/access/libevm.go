// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package access

import (
	"math/big"

	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/pseudo"
)

var extras = ethtypes.RegisterExtras[
	customtypes.HeaderExtra, *customtypes.HeaderExtra,
	customtypes.BlockBodyExtra, *customtypes.BlockBodyExtra,
	customtypes.StateAccountExtraType,
]()

func IsMultiCoinAccessor() pseudo.Accessor[ethtypes.StateOrSlimAccount, customtypes.StateAccountExtraType] {
	return extras.StateAccount
}

func IsMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return extras.StateAccount.Get(s).IsMultiCoin()
}

// SetBlockExtra sets the [BlockBodyExtra] `extra` in the [Block] `b`.
func SetBlockExtra(b *ethtypes.Block, extra *customtypes.BlockBodyExtra) {
	extras.Block.Set(b, extra)
}

func BlockExtData(b *ethtypes.Block) []byte {
	if data := extras.Block.Get(b).ExtData; data != nil {
		return *data
	}
	return nil
}

func BlockVersion(b *ethtypes.Block) uint32 {
	return extras.Block.Get(b).Version
}

func NewBlockWithExtData(
	header *ethtypes.Header, txs []*ethtypes.Transaction, uncles []*ethtypes.Header, receipts []*ethtypes.Receipt,
	hasher ethtypes.TrieHasher, extdata []byte, recalc bool,
) *ethtypes.Block {
	if recalc {
		headerExtra := GetHeaderExtra(header)
		headerExtra.ExtDataHash = customtypes.CalcExtDataHash(extdata)
	}
	block := ethtypes.NewBlock(header, txs, uncles, receipts, hasher)
	extdataCopy := make([]byte, len(extdata))
	copy(extdataCopy, extdata)
	extra := &customtypes.BlockBodyExtra{
		ExtData: &extdataCopy,
	}
	extras.Block.Set(block, extra)
	return block
}

// GetHeaderExtra returns the [HeaderExtra] from the given [Header].
func GetHeaderExtra(h *ethtypes.Header) *customtypes.HeaderExtra {
	return extras.Header.Get(h)
}

// SetHeaderExtra sets the given [HeaderExtra] on the [Header].
func SetHeaderExtra(h *ethtypes.Header, extra *customtypes.HeaderExtra) {
	extras.Header.Set(h, extra)
}

// WithHeaderExtra sets the given [HeaderExtra] on the [Header]
// and returns the [Header] for chaining.
func WithHeaderExtra(h *ethtypes.Header, extra *customtypes.HeaderExtra) *ethtypes.Header {
	SetHeaderExtra(h, extra)
	return h
}

func BlockExtDataGasUsed(b *ethtypes.Block) *big.Int {
	used := GetHeaderExtra(b.Header()).ExtDataGasUsed
	if used == nil {
		return nil
	}
	return new(big.Int).Set(used)
}

func BlockGasCost(b *ethtypes.Block) *big.Int {
	cost := GetHeaderExtra(b.Header()).BlockGasCost
	if cost == nil {
		return nil
	}
	return new(big.Int).Set(cost)
}
