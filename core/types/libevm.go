// (c) 2024-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

type isMultiCoin bool

var (
	extras = ethtypes.RegisterExtras[
		HeaderExtra, *HeaderExtra,
		BodyExtra, *BodyExtra,
		BlockExtra, *BlockExtra,
		isMultiCoin]()
	IsMultiCoinPayloads = extras.StateAccount
)

func IsMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return bool(extras.StateAccount.Get(s))
}

func HeaderExtras(h *Header) *HeaderExtra {
	return extras.Header.Get(h)
}

func WithHeaderExtras(h *Header, extra *HeaderExtra) *Header {
	extras.Header.Set(h, extra)
	return h
}

func GetBodyExtra(b *Body) *BodyExtra {
	return extras.Body.Get(b)
}

func WithBodyExtra(b *Body, extra *BodyExtra) *Body {
	extras.Body.Set(b, extra)
	return b
}

func GetBlockExtra(b *Block) *BlockExtra {
	return extras.Block.Get(b)
}

func WithBlockExtra(b *Block, version uint32, extdata *[]byte, recalc bool) *Block {
	extras := GetBlockExtra(b)

	extras.version = version

	cpy := make([]byte, 0)
	if extdata != nil {
		cpy = make([]byte, len(*extdata))
		copy(cpy, *extdata)
	}

	extras.extdata = &cpy
	if recalc {
		header := b.Header()
		headerExtra := HeaderExtras(header)
		headerExtra.ExtDataHash = CalcExtDataHash(cpy)
		b.SetHeader(header)
	}

	return b
}
