// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"io"

	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// BodyExtra is a struct that contains extra fields used by Avalanche
// in the body.
// This type uses BodySerializable to encode and decode the extra fields
// along with the upstream type for compatibility with existing network blocks.
type BodyExtra struct {
	Version uint32
	ExtData *[]byte

	// Fields removed from geth:
	//  - withdrawals  Withdrawals
}

func (b *BodyExtra) EncodeRLP(eth *ethtypes.Body, writer io.Writer) error {
	out := new(bodySerializable)

	out.updateFromEth(eth)
	out.updateFromExtras(b)

	return rlp.Encode(writer, out)
}

func (b *BodyExtra) DecodeRLP(eth *ethtypes.Body, stream *rlp.Stream) error {
	in := new(bodySerializable)
	if err := stream.Decode(in); err != nil {
		return err
	}

	in.updateToEth(eth)
	in.updateToExtras(b)

	return nil
}

// bodySerializable defines the body in the Ethereum blockchain,
// as it is to be serialized into RLP.
type bodySerializable struct {
	Transactions []*Transaction
	Uncles       []*Header
	Version      uint32
	ExtData      *[]byte `rlp:"nil"`
}

// updateFromEth updates the [*bodySerializable] from the [*ethtypes.Body].
func (b *bodySerializable) updateFromEth(eth *ethtypes.Body) {
	b.Transactions = eth.Transactions
	b.Uncles = eth.Uncles
}

// updateToEth updates the [*ethtypes.Body] from the [*bodySerializable].
func (b *bodySerializable) updateToEth(eth *ethtypes.Body) {
	eth.Transactions = b.Transactions
	eth.Uncles = b.Uncles
}

// updateFromExtras updates the [*bodySerializable] from the [*BodyExtra].
func (b *bodySerializable) updateFromExtras(extras *BodyExtra) {
	b.Version = extras.Version
	b.ExtData = extras.ExtData
}

// updateToExtras updates the [*BodyExtra] from the [*bodySerializable].
func (b *bodySerializable) updateToExtras(extras *BodyExtra) {
	extras.Version = b.Version
	extras.ExtData = b.ExtData
}
