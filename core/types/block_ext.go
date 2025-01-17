// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// GetBlockExtra returns the [BlockBodyExtra] contained in the [Block] `b`.
func GetBlockExtra(b *Block) *BlockBodyExtra {
	return extras.Block.Get(b)
}

// WithBlockExtra sets the [BlockBodyExtra] `extra` into the [Block] `b`.
func WithBlockExtra(b *Block, extra *BlockBodyExtra) *Block {
	extras.Block.Set(b, extra)
	return b
}

// GetBodyExtra returns the [BlockBodyExtra] contained in the [Body] `b`.
func GetBodyExtra(b *Body) *BlockBodyExtra {
	return extras.Body.Get(b)
}

// WithBodyExtra sets the [BlockBodyExtra] `extra` into the [Body] `b`.
func WithBodyExtra(b *Body, extra *BlockBodyExtra) *Body {
	extras.Body.Set(b, extra)
	return b
}

// BlockBodyExtra is a struct containing extra fields used by Avalanche
// in the [Block] and [Body].
type BlockBodyExtra struct {
	Version uint32
	ExtData *[]byte

	// RLP block and body field removed from geth:
	// - Withdrawals []*Withdrawal
}

// Copy deep copies the [BlockBodyExtra] `b` and returns it.
// It is notably used in the following functions:
// - [Block]'s Body() method
// - [Block]'s With*() methods
func (b *BlockBodyExtra) Copy() *BlockBodyExtra {
	cpy := *b
	if b.ExtData == nil {
		data := make([]byte, 0)
		cpy.ExtData = &data
	} else {
		data := make([]byte, len(*b.ExtData))
		copy(data, *b.ExtData)
		cpy.ExtData = &data
	}
	return &cpy
}

// BodyRLPFieldPointersForEncoding returns the fields that should be encoded
// for the [Body] and [BlockBodyExtra].
func (b *BlockBodyExtra) BodyRLPFieldsForEncoding(body *Body) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			body.Transactions,
			body.Uncles,
			b.Version,
			b.ExtData,
		},
	}
}

// BodyRLPFieldPointersForDecoding returns the fields that should be decoded to
// for the [Body] and [BlockBodyExtra].
func (b *BlockBodyExtra) BodyRLPFieldPointersForDecoding(body *Body) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			&body.Transactions,
			&body.Uncles,
			&b.Version,
			&b.ExtData,
		},
	}
}

// BlockRLPFieldPointersForEncoding returns the fields that should be encoded
// for the [Block] and [BlockBodyExtra].
func (b *BlockBodyExtra) BlockRLPFieldsForEncoding(block *ethtypes.BlockRLPProxy) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			block.Header,
			block.Txs,
			block.Uncles,
			b.Version,
			b.ExtData,
		},
	}
}

// BlockRLPFieldPointersForDecoding returns the fields that should be decoded to
// for the [Block] and [BlockBodyExtra].
func (b *BlockBodyExtra) BlockRLPFieldPointersForDecoding(block *ethtypes.BlockRLPProxy) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			&block.Header,
			&block.Txs,
			&block.Uncles,
			&b.Version,
			&b.ExtData,
		},
	}
}

func BlockExtData(b *Block) []byte {
	extras := GetBlockExtra(b)
	if extras.ExtData == nil {
		return nil
	}
	return *extras.ExtData
}

func BlockVersion(b *Block) uint32 {
	return GetBlockExtra(b).Version
}

func BlockExtDataGasUsed(b *Block) *big.Int {
	used := GetHeaderExtra(b.Header()).ExtDataGasUsed
	if used == nil {
		return nil
	}
	return new(big.Int).Set(used)
}

func BlockGasCost(b *Block) *big.Int {
	header := b.Header() // note this deep copies headerExtra.BlockGasCost
	headerExtra := GetHeaderExtra(header)
	return headerExtra.BlockGasCost
}

func CalcExtDataHash(extdata []byte) common.Hash {
	if len(extdata) == 0 {
		return EmptyExtDataHash
	}
	return rlpHash(extdata)
}

func NewBlockWithExtData(
	header *Header, txs []*Transaction, uncles []*Header, receipts []*Receipt,
	hasher TrieHasher, extdata []byte, recalc bool,
) *Block {
	if recalc {
		headerExtra := GetHeaderExtra(header)
		headerExtra.ExtDataHash = CalcExtDataHash(extdata)
	}
	block := NewBlock(header, txs, uncles, receipts, hasher)
	extdataCopy := make([]byte, len(extdata))
	copy(extdataCopy, extdata)
	extras := &BlockBodyExtra{
		ExtData: &extdataCopy,
	}
	return WithBlockExtra(block, extras)
}
