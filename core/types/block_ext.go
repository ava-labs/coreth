package types

import (
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

type (
	Withdrawal  = types.Withdrawal
	Withdrawals = types.Withdrawals

	_Header Header
)

var (
	EmptyWithdrawalsHash = types.EmptyWithdrawalsHash
)

func (b *Block) WithBodyExt(version uint32, extdata *[]byte) *Block {
	b.version = version
	b.setExtDataHelper(extdata, false)
	return b
}

func (b *Block) setExtDataHelper(data *[]byte, recalc bool) {
	if data == nil {
		b.setExtData(nil, recalc)
		return
	}
	b.setExtData(*data, recalc)
}

func (b *Block) setExtData(data []byte, recalc bool) {
	_data := make([]byte, len(data))
	b.extdata = &_data
	copy(*b.extdata, data)
	if recalc {
		b.header.ExtDataHash = CalcExtDataHash(*b.extdata)
	}
}

func (b *Block) ExtData() []byte {
	if b.extdata == nil {
		return nil
	}
	return *b.extdata
}

func (b *Block) Version() uint32 {
	return b.version
}

func (b *Block) Timestamp() uint64 { return b.header.Time }

func (b *Block) ExtDataGasUsed() *big.Int {
	if b.header.ExtDataGasUsed == nil {
		return nil
	}
	return new(big.Int).Set(b.header.ExtDataGasUsed)
}

func (b *Block) BlockGasCost() *big.Int {
	if b.header.BlockGasCost == nil {
		return nil
	}
	return new(big.Int).Set(b.header.BlockGasCost)
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
	b := NewBlock(header, txs, uncles, receipts, hasher)
	b.setExtData(extdata, recalc)
	return b
}

var (
	BlockEncodeRLP  func(b *Block, w io.Writer) error
	BlockDecodeRLP  func(b *Block, s *rlp.Stream) error
	BodyDecodeRLP   func(b *Body, s *rlp.Stream) error
	BodyEncodeRLP   func(b *Body, w io.Writer) error
	HeaderEncodeRLP func(h *Header, w io.Writer) error
	HeaderDecodeRLP func(h *Header, s *rlp.Stream) error
)

// "external" block encoding. used for eth protocol, etc.
type extblock_coreth struct {
	Header  *Header
	Txs     []*Transaction
	Uncles  []*Header
	Version uint32
	ExtData *[]byte `rlp:"nil"`
}

func BlockEncodeRLP_Coreth(b *Block, w io.Writer) error {
	return rlp.Encode(w, &extblock_coreth{
		Header:  b.header,
		Txs:     b.transactions,
		Uncles:  b.uncles,
		Version: b.version,
		ExtData: b.extdata,
	})
}

func BlockDecodeRLP_Coreth(b *Block, s *rlp.Stream) error {
	var eb extblock_coreth
	_, size, _ := s.Kind()
	if err := s.Decode(&eb); err != nil {
		return err
	}
	b.header, b.uncles, b.transactions, b.version, b.extdata = eb.Header, eb.Uncles, eb.Txs, eb.Version, eb.ExtData
	b.size.Store(rlp.ListSize(size))
	return nil
}

type extbody_coreth struct {
	Transactions []*Transaction
	Uncles       []*Header
	Version      uint32
	ExtData      *[]byte `rlp:"nil"`
}

func BodyEncodeRLP_Coreth(b *Body, w io.Writer) error {
	return rlp.Encode(w, &extbody_coreth{
		Transactions: b.Transactions,
		Uncles:       b.Uncles,
		Version:      b.Version,
		ExtData:      b.ExtData,
	})
}

func BodyDecodeRLP_Coreth(b *Body, s *rlp.Stream) error {
	var eb extbody_coreth
	if err := s.Decode(&eb); err != nil {
		return err
	}
	b.Transactions, b.Uncles, b.Version, b.ExtData = eb.Transactions, eb.Uncles, eb.Version, eb.ExtData
	return nil
}

func (b *Body) EncodeRLP(w io.Writer) error {
	if BodyEncodeRLP != nil {
		return BodyEncodeRLP(b, w)
	}
	type _Body Body
	return rlp.Encode(w, (*_Body)(b))
}

func (b *Body) DecodeRLP(s *rlp.Stream) error {
	if BodyDecodeRLP != nil {
		return BodyDecodeRLP(b, s)
	}
	type _Body Body
	return s.Decode((*_Body)(b))
}

type extheader_coreth struct {
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

	// ExcessDataGas was added by EIP-4844 and is ignored in legacy headers.
	ExcessDataGas *big.Int `json:"excessDataGas" rlp:"optional"`
}

func HeaderEncodeRLP_Coreth(h *Header, w io.Writer) error {
	return rlp.Encode(w, &extheader_coreth{
		ParentHash:     h.ParentHash,
		UncleHash:      h.UncleHash,
		Coinbase:       h.Coinbase,
		Root:           h.Root,
		TxHash:         h.TxHash,
		ReceiptHash:    h.ReceiptHash,
		Bloom:          h.Bloom,
		Difficulty:     h.Difficulty,
		Number:         h.Number,
		GasLimit:       h.GasLimit,
		GasUsed:        h.GasUsed,
		Time:           h.Time,
		Extra:          h.Extra,
		MixDigest:      h.MixDigest,
		Nonce:          h.Nonce,
		ExtDataHash:    h.ExtDataHash,
		BaseFee:        h.BaseFee,
		ExtDataGasUsed: h.ExtDataGasUsed,
		BlockGasCost:   h.BlockGasCost,
		ExcessDataGas:  h.ExcessDataGas,
	})
}

func HeaderDecodeRLP_Coreth(h *Header, s *rlp.Stream) error {
	var eh extheader_coreth
	if err := s.Decode(&eh); err != nil {
		return err
	}
	h.ParentHash = eh.ParentHash
	h.UncleHash = eh.UncleHash
	h.Coinbase = eh.Coinbase
	h.Root = eh.Root
	h.TxHash = eh.TxHash
	h.ReceiptHash = eh.ReceiptHash
	h.Bloom = eh.Bloom
	h.Difficulty = eh.Difficulty
	h.Number = eh.Number
	h.GasLimit = eh.GasLimit
	h.GasUsed = eh.GasUsed
	h.Time = eh.Time
	h.Extra = eh.Extra
	h.MixDigest = eh.MixDigest
	h.Nonce = eh.Nonce
	h.ExtDataHash = eh.ExtDataHash
	h.BaseFee = eh.BaseFee
	h.ExtDataGasUsed = eh.ExtDataGasUsed
	h.BlockGasCost = eh.BlockGasCost
	h.ExcessDataGas = eh.ExcessDataGas
	return nil
}

func (h *Header) EncodeRLP(w io.Writer) error {
	if HeaderEncodeRLP != nil {
		return HeaderEncodeRLP(h, w)
	}
	return rlp.Encode(w, (*_Header)(h))
}

func (h *Header) DecodeRLP(s *rlp.Stream) error {
	if HeaderDecodeRLP != nil {
		return HeaderDecodeRLP(h, s)
	}
	return s.Decode((*_Header)(h))
}

func init() {
	BlockEncodeRLP = BlockEncodeRLP_Coreth
	BlockDecodeRLP = BlockDecodeRLP_Coreth
	BodyEncodeRLP = BodyEncodeRLP_Coreth
	BodyDecodeRLP = BodyDecodeRLP_Coreth
	HeaderEncodeRLP = HeaderEncodeRLP_Coreth
	HeaderDecodeRLP = HeaderDecodeRLP_Coreth
}
