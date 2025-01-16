// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"io"
	"math/big"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// BlockExtra is a struct that contains extra fields used by Avalanche
// in the block.
// This type uses BlockSerializable to encode and decode the extra fields
// along with the upstream type for compatibility with existing network blocks.
type BlockExtra struct {
	version uint32
	extdata *[]byte

	// Fields removed from geth:
	//  - withdrawals  Withdrawals
	//  - ReceivedAt   time.Time
	//  - ReceivedFrom interface{}
}

func (b *BlockExtra) EncodeRLP(eth *ethtypes.Block, writer io.Writer) error {
	out := new(blockSerializable)

	out.updateFromEth(eth)
	out.updateFromExtras(b)

	return rlp.Encode(writer, out)
}

func (b *BlockExtra) DecodeRLP(eth *ethtypes.Block, stream *rlp.Stream) error {
	in := new(blockSerializable)
	if err := stream.Decode(in); err != nil {
		return err
	}

	in.updateToEth(eth)
	in.updateToExtras(b)

	return nil
}

func (b *BlockExtra) Body(block *Block) *Body {
	body := block.EthBody()
	extra := &BodyExtra{
		Version: b.version,
		ExtData: b.extdata,
	}
	return WithBodyExtra(body, extra)
}

// blockSerializable defines the block in the Ethereum blockchain,
// as it is to be serialized into RLP.
type blockSerializable struct {
	Header  *Header
	Txs     []*Transaction
	Uncles  []*Header
	Version uint32
	ExtData *[]byte `rlp:"nil"`
}

// updateFromEth updates the [*blockSerializable] from the [*ethtypes.Block].
func (b *blockSerializable) updateFromEth(eth *ethtypes.Block) {
	b.Header = eth.Header() // note this deep copies the header
	b.Uncles = eth.Uncles()
	b.Txs = eth.Transactions()
}

// updateToEth updates the [*ethtypes.Block] from the [*blockSerializable].
func (b *blockSerializable) updateToEth(eth *ethtypes.Block) {
	eth.SetHeader(b.Header)
	eth.SetTransactions(b.Txs)
	eth.SetUncles(b.Uncles)
}

// updateFromExtras updates the [*blockSerializable] from the [*BlockExtra].
func (b *blockSerializable) updateFromExtras(extras *BlockExtra) {
	b.Version = extras.version
	b.ExtData = extras.extdata
}

// updateToExtras updates the [*BlockExtra] from the [*blockSerializable].
func (b *blockSerializable) updateToExtras(extras *BlockExtra) {
	extras.version = b.Version
	extras.extdata = b.ExtData
}

func BlockWithExtData(b *Block, version uint32, extdata *[]byte) *Block {
	BlockExtras(b).version = version
	setExtDataHelper(b, extdata, false)
	return b
}

func setExtDataHelper(b *Block, data *[]byte, recalc bool) {
	if data == nil {
		setExtData(b, nil, recalc)
		return
	}
	setExtData(b, *data, recalc)
}

func setExtData(b *Block, data []byte, recalc bool) {
	_data := make([]byte, len(data))
	extras := BlockExtras(b)
	extras.extdata = &_data
	copy(*extras.extdata, data)
	if recalc {
		header := b.Header()
		headerExtra := HeaderExtras(header)
		headerExtra.ExtDataHash = CalcExtDataHash(_data)
		b.SetHeader(header)
	}
}

func BlockExtData(b *Block) []byte {
	extras := BlockExtras(b)
	if extras.extdata == nil {
		return nil
	}
	return *extras.extdata
}

func BlockVersion(b *Block) uint32 {
	return BlockExtras(b).version
}

func BlockExtDataGasUsed(b *Block) *big.Int {
	if HeaderExtras(b.Header()).ExtDataGasUsed == nil {
		return nil
	}
	return new(big.Int).Set(HeaderExtras(b.Header()).ExtDataGasUsed)
}

func BlockGasCost(b *Block) *big.Int {
	header := b.Header() // note this deep copies headerExtra.BlockGasCost
	headerExtra := HeaderExtras(header)
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
	b := ethtypes.NewBlock(header, txs, uncles, receipts, hasher)
	const version = 0
	return WithBlockExtras(b, version, &extdata, recalc)
}

func WrapWithTimestamp(b *Block) *BlockWithTimestamp {
	return &BlockWithTimestamp{Block: b}
}

type BlockWithTimestamp struct {
	*Block
}

func (b *BlockWithTimestamp) Timestamp() uint64 {
	return b.Time()
}
