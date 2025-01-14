// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"io"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// HeaderExtra is a struct that contains extra fields used by Avalanche
// in the block header.
// This type uses HeaderSerializable to encode and decode the extra fields
// along with the upstream type for compatibility with existing network blocks.
type HeaderExtra struct {
	ExtDataHash    common.Hash
	ExtDataGasUsed *big.Int
	BlockGasCost   *big.Int
}

func (h *HeaderExtra) EncodeRLP(eth *ethtypes.Header, writer io.Writer) error {
	out := new(HeaderSerializable)

	updateFromEth(out, eth)
	updateFromExtras(out, h)

	return rlp.Encode(writer, out)
}

func (h *HeaderExtra) DecodeRLP(eth *ethtypes.Header, stream *rlp.Stream) error {
	in := new(HeaderSerializable)
	if err := stream.Decode(in); err != nil {
		return err
	}

	updateToEth(in, eth)
	updateToExtras(in, h)

	return nil
}

//nolint:stdmethods
func (h *HeaderExtra) MarshalJSON(eth *ethtypes.Header) ([]byte, error) {
	out := new(HeaderSerializable)

	updateFromEth(out, eth)
	updateFromExtras(out, h)

	return out.MarshalJSON()
}

//nolint:stdmethods
func (h *HeaderExtra) UnmarshalJSON(eth *ethtypes.Header, input []byte) error {
	in := new(HeaderSerializable)
	if err := in.UnmarshalJSON(input); err != nil {
		return err
	}

	updateToEth(in, eth)
	updateToExtras(in, h)

	return nil
}

//go:generate go run github.com/fjl/gencodec -type HeaderSerializable -field-override headerMarshaling -out gen_header_json.go
//go:generate go run github.com/ava-labs/libevm/rlp/rlpgen -type HeaderSerializable -out gen_header_rlp.go

// HeaderSerializable defines the header of a block in the Ethereum blockchain,
// as it is to be serialized into RLP and JSON.
type HeaderSerializable struct {
	ParentHash  common.Hash    `json:"parentHash"       gencodec:"required"`
	UncleHash   common.Hash    `json:"sha3Uncles"       gencodec:"required"`
	Coinbase    common.Address `json:"miner"            gencodec:"required"`
	Root        common.Hash    `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash    `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash    `json:"receiptsRoot"     gencodec:"required"`
	Bloom       Bloom          `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int       `json:"difficulty"       gencodec:"required"`
	Number      *big.Int       `json:"number"           gencodec:"required"`
	GasLimit    uint64         `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64         `json:"gasUsed"          gencodec:"required"`
	Time        uint64         `json:"timestamp"        gencodec:"required"`
	Extra       []byte         `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash    `json:"mixHash"`
	Nonce       BlockNonce     `json:"nonce"`
	ExtDataHash common.Hash    `json:"extDataHash"      gencodec:"required"`

	// BaseFee was added by EIP-1559 and is ignored in legacy headers.
	BaseFee *big.Int `json:"baseFeePerGas" rlp:"optional"`

	// ExtDataGasUsed was added by Apricot Phase 4 and is ignored in legacy
	// headers.
	//
	// It is not a uint64 like GasLimit or GasUsed because it is not possible to
	// correctly encode this field optionally with uint64.
	ExtDataGasUsed *big.Int `json:"extDataGasUsed" rlp:"optional"`

	// BlockGasCost was added by Apricot Phase 4 and is ignored in legacy
	// headers.
	BlockGasCost *big.Int `json:"blockGasCost" rlp:"optional"`

	// BlobGasUsed was added by EIP-4844 and is ignored in legacy headers.
	BlobGasUsed *uint64 `json:"blobGasUsed" rlp:"optional"`

	// ExcessBlobGas was added by EIP-4844 and is ignored in legacy headers.
	ExcessBlobGas *uint64 `json:"excessBlobGas" rlp:"optional"`

	// ParentBeaconRoot was added by EIP-4788 and is ignored in legacy headers.
	ParentBeaconRoot *common.Hash `json:"parentBeaconBlockRoot" rlp:"optional"`
}

// field type overrides for gencodec
type headerMarshaling struct {
	Difficulty     *hexutil.Big
	Number         *hexutil.Big
	GasLimit       hexutil.Uint64
	GasUsed        hexutil.Uint64
	Time           hexutil.Uint64
	Extra          hexutil.Bytes
	BaseFee        *hexutil.Big
	ExtDataGasUsed *hexutil.Big
	BlockGasCost   *hexutil.Big
	Hash           common.Hash `json:"hash"` // adds call to Hash() in MarshalJSON
	BlobGasUsed    *hexutil.Uint64
	ExcessBlobGas  *hexutil.Uint64
}

// Hash returns the block hash of the header, which is simply the keccak256 hash of its
// RLP encoding.
func (h *HeaderSerializable) Hash() common.Hash {
	return rlpHash(h)
}

// updateFromEth updates the HeaderSerializable from the ethtypes.Header
func updateFromEth(h *HeaderSerializable, eth *ethtypes.Header) {
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

// updateToEth updates the ethtypes.Header from the HeaderSerializable
func updateToEth(h *HeaderSerializable, eth *ethtypes.Header) {
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

// updateFromExtras updates the HeaderSerializable from the HeaderExtra
func updateFromExtras(h *HeaderSerializable, extras *HeaderExtra) {
	h.ExtDataHash = extras.ExtDataHash
	h.ExtDataGasUsed = extras.ExtDataGasUsed
	h.BlockGasCost = extras.BlockGasCost
}

// updateToExtras updates the HeaderExtra from the HeaderSerializable
func updateToExtras(h *HeaderSerializable, extras *HeaderExtra) {
	extras.ExtDataHash = h.ExtDataHash
	extras.ExtDataGasUsed = h.ExtDataGasUsed
	extras.BlockGasCost = h.BlockGasCost
}
