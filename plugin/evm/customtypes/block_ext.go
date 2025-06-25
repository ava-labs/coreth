// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"slices"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// BlockBodyExtra is a struct containing extra fields used by Avalanche
// in the [Block] and [Body].
type BlockBodyExtra struct {
	Version uint32
	ExtData *[]byte
}

// Copy deep copies the [BlockBodyExtra] `b` and returns it.
// It is notably used in the following functions:
// - [ethtypes.Block.Body]
// - [ethtypes.Block.WithSeal]
// - [ethtypes.Block.WithBody]
// - [ethtypes.Block.WithWithdrawals]
func (b *BlockBodyExtra) Copy() *BlockBodyExtra {
	cpy := *b
	if b.ExtData != nil {
		data := slices.Clone(*b.ExtData)
		cpy.ExtData = &data
	}
	return &cpy
}

// BodyRLPFieldPointersForEncoding returns the fields that should be encoded
// for the [Body] and [BlockBodyExtra].
// Note the following fields are added (+) and removed (-) compared to geth:
// - (-) [ethtypes.Body] `Withdrawals` field
// - (+) [BlockBodyExtra] `Version` field
// - (+) [BlockBodyExtra] `ExtData` field
func (b *BlockBodyExtra) BodyRLPFieldsForEncoding(body *ethtypes.Body) *rlp.Fields {
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
func (b *BlockBodyExtra) BodyRLPFieldPointersForDecoding(body *ethtypes.Body) *rlp.Fields {
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
// Note the following fields are added (+) and removed (-) compared to geth:
// - (-) [ethtypes.Block] `Withdrawals` field
// - (+) [BlockBodyExtra] `Version` field
// - (+) [BlockBodyExtra] `ExtData` field
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

func CalcExtDataHash(extdata []byte) common.Hash {
	if len(extdata) == 0 {
		return EmptyExtDataHash
	}
	return ethtypes.RLPHash(extdata)
}
