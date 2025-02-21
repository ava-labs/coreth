// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"math/big"
	"reflect"
	"testing"
	"unsafe"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyHeader(t *testing.T) {
	t.Parallel()

	t.Run("empty_header", func(t *testing.T) {
		t.Parallel()

		empty := &Header{}

		headerExtra := &HeaderExtra{}
		extras.Header.Set(empty, headerExtra)

		cpy := CopyHeader(empty)

		want := &Header{
			Difficulty: new(big.Int),
			Number:     new(big.Int),
		}

		headerExtra = &HeaderExtra{}
		extras.Header.Set(want, headerExtra)

		assert.Equal(t, want, cpy)
	})

	t.Run("filled_header", func(t *testing.T) {
		t.Parallel()

		header, _ := headerWithNonZeroFields() // the header carries the [HeaderExtra] so we can ignore it

		gotHeader := CopyHeader(header)
		gotExtra := GetHeaderExtra(gotHeader)

		wantHeader, wantExtra := headerWithNonZeroFields()
		assert.Equal(t, wantHeader, gotHeader)
		assert.Equal(t, wantExtra, gotExtra)

		exportedFieldsPointToDifferentMemory(t, header, gotHeader)
		exportedFieldsPointToDifferentMemory(t, GetHeaderExtra(header), gotExtra)
	})
}

func exportedFieldsPointToDifferentMemory[T interface {
	Header | HeaderExtra | BlockBodyExtra
}](t *testing.T, original, cpy *T) {
	t.Helper()

	v := reflect.ValueOf(*original)
	typ := v.Type()
	cp := reflect.ValueOf(*cpy)
	for i := range v.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		switch field.Type.Kind() {
		case reflect.Array, reflect.Uint64, reflect.Uint32:
			// Not pointers, but using explicit Kinds for safety
			continue
		}

		t.Run(field.Name, func(t *testing.T) {
			fieldCp := cp.Field(i).Interface()
			switch f := v.Field(i).Interface().(type) {
			case *big.Int:
				assertDifferentPointers(t, f, fieldCp)
			case *common.Hash:
				assertDifferentPointers(t, f, fieldCp)
			case *uint64:
				assertDifferentPointers(t, f, fieldCp)
			case *[]uint8:
				assertDifferentPointers(t, f, fieldCp)
			case []uint8:
				assertDifferentPointers(t, unsafe.SliceData(f), unsafe.SliceData(fieldCp.([]uint8)))
			default:
				t.Errorf("field %q type %T needs to be added to switch cases of exportedFieldsDeepCopied", field.Name, f)
			}
		})
	}
}

// assertDifferentPointers asserts that `a` and `b` are both non-nil
// pointers pointing to different memory locations.
func assertDifferentPointers[T any](t *testing.T, a *T, b any) {
	t.Helper()
	switch {
	case a == nil:
		t.Errorf("a (%T) cannot be nil", a)
	case b == nil:
		t.Errorf("b (%T) cannot be nil", b)
	case a == b:
		t.Errorf("pointers to same memory")
	}
	// Note: no need to check `b` is of the same type as `a`, otherwise
	// the memory address would be different as well.
}

func TestBlockExtraGetWith(t *testing.T) {
	t.Parallel()

	b := &Block{}

	extra := extras.Block.Get(b)
	require.NotNil(t, extra)
	assert.Equal(t, &BlockBodyExtra{}, extra)

	extra = &BlockBodyExtra{
		Version: 1,
	}
	extras.Block.Set(b, extra)

	extra = extras.Block.Get(b)
	wantExtra := &BlockBodyExtra{
		Version: 1,
	}
	assert.Equal(t, wantExtra, extra)
}

func TestBodyExtraGetWith(t *testing.T) {
	t.Parallel()

	b := &Body{}

	extra := extras.Body.Get(b)
	require.NotNil(t, extra)
	assert.Equal(t, &BlockBodyExtra{}, extra)

	extra = &BlockBodyExtra{
		Version: 1,
	}
	extras.Body.Set(b, extra)

	extra = extras.Body.Get(b)
	wantExtra := &BlockBodyExtra{
		Version: 1,
	}
	assert.Equal(t, wantExtra, extra)
}

func TestBodyExtraRLP(t *testing.T) {
	t.Parallel()

	testTx := NewTransaction(0, common.Address{1}, big.NewInt(2), 3, big.NewInt(4), []byte{5})

	uncle := &Header{ParentHash: common.Hash{6}}
	uncleExtra := &HeaderExtra{ExtDataHash: common.Hash{7}}
	SetHeaderExtra(uncle, uncleExtra)

	body := &Body{
		Transactions: []*Transaction{testTx},
		Uncles:       []*Header{uncle},
		Withdrawals:  []*ethtypes.Withdrawal{{Index: 7}}, // ignored
	}
	extra := &BlockBodyExtra{
		Version: 1,
		ExtData: ptrTo([]byte{2}),
	}
	extras.Body.Set(body, extra)

	b, err := rlp.EncodeToBytes(body)
	require.NoError(t, err)

	gotBody := new(Body)
	err = rlp.DecodeBytes(b, gotBody)
	require.NoError(t, err)

	// Check transactions in the following because unexported fields
	// size and time are set by the RLP decoder.
	require.Len(t, gotBody.Transactions, 1)
	gotTx := gotBody.Transactions[0]
	gotTxJSON, err := gotTx.MarshalJSON()
	require.NoError(t, err)
	wantTxJSON, err := testTx.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, string(wantTxJSON), string(gotTxJSON))
	gotBody.Transactions = nil

	wantUncle := &Header{
		ParentHash: common.Hash{6},
		Difficulty: new(big.Int),
		Number:     new(big.Int),
		Extra:      []byte{},
	}
	wantUncleExtra := &HeaderExtra{ExtDataHash: common.Hash{7}}
	SetHeaderExtra(wantUncle, wantUncleExtra)

	wantBody := &Body{
		Transactions: nil, // checked above
		Uncles:       []*Header{wantUncle},
	}
	wantExtra := &BlockBodyExtra{
		Version: 1,
		ExtData: ptrTo([]byte{2}),
	}
	extras.Body.Set(wantBody, wantExtra)

	assert.Equal(t, wantBody, gotBody)
	gotExtra := extras.Body.Get(gotBody)
	assert.Equal(t, wantExtra, gotExtra)
}

func TestBlockExtraRLP(t *testing.T) {
	t.Parallel()

	header := &Header{ParentHash: common.Hash{1}}
	headerExtras := &HeaderExtra{ExtDataHash: common.Hash{2}}
	SetHeaderExtra(header, headerExtras)

	testTx := NewTransaction(3, common.Address{4}, big.NewInt(2), 3, big.NewInt(4), []byte{5})
	txs := []*Transaction{testTx}

	uncle := &Header{ParentHash: common.Hash{6}}
	uncleExtra := &HeaderExtra{ExtDataHash: common.Hash{7}}
	SetHeaderExtra(uncle, uncleExtra)
	uncles := []*Header{uncle}

	receipts := []*Receipt{{PostState: []byte{8}}}

	block := NewBlock(header, txs, uncles, receipts, stubHasher{})

	withdrawals := []*ethtypes.Withdrawal{{Index: 9}} // ignored
	block = block.WithWithdrawals(withdrawals)

	extra := &BlockBodyExtra{
		Version: 10,
		ExtData: ptrTo([]byte{11}),
	}
	extras.Block.Set(block, extra)

	b, err := rlp.EncodeToBytes(block)
	require.NoError(t, err)

	gotBlock := new(Block)
	err = rlp.DecodeBytes(b, gotBlock)
	require.NoError(t, err)

	// Check transactions in the following because unexported fields
	// size and time are set by the RLP decoder.
	gotTxs := gotBlock.Transactions()
	require.Len(t, gotTxs, 1)
	gotTx := gotTxs[0]
	gotTxBin, err := gotTx.MarshalBinary()
	require.NoError(t, err)
	wantTxBin, err := testTx.MarshalBinary()
	require.NoError(t, err)
	assert.Equal(t, wantTxBin, gotTxBin)

	wantHeader := &Header{
		ParentHash: common.Hash{1},
		Extra:      []byte{}, // field set by RLP decoder
	}
	wantHeaderExtras := &HeaderExtra{ExtDataHash: common.Hash{2}}
	SetHeaderExtra(wantHeader, wantHeaderExtras)

	wantUncle := &Header{
		ParentHash: common.Hash{6},
		Difficulty: new(big.Int), // field set by RLP decoder
		Number:     new(big.Int), // field set by RLP decoder
		Extra:      []byte{},     // field set by RLP decoder
	}
	wantUncleExtra := &HeaderExtra{ExtDataHash: common.Hash{7}}
	SetHeaderExtra(wantUncle, wantUncleExtra)
	wantUncles := []*Header{wantUncle}

	wantBlock := NewBlock(wantHeader, gotTxs /* checked above */, wantUncles, receipts, stubHasher{})
	wantExtra := &BlockBodyExtra{
		Version: 10,
		ExtData: ptrTo([]byte{11}),
	}
	extras.Block.Set(wantBlock, wantExtra)
	_ = wantBlock.Size() // set block cached unexported size field

	assert.Equal(t, wantBlock, gotBlock)
	gotExtra := extras.Block.Get(gotBlock)
	assert.Equal(t, wantExtra, gotExtra)
}

// TestBlockBody tests the [BlockBodyExtra.Copy] method is implemented correctly.
func TestBlockBody(t *testing.T) {
	t.Parallel()

	header := &Header{ParentHash: common.Hash{1}}
	tx := NewTransaction(0, common.Address{1}, big.NewInt(2), 3, big.NewInt(4), []byte{5})
	txs := []*Transaction{tx}
	uncles := []*Header{{ParentHash: common.Hash{6}}}
	receipts := []*Receipt{{PostState: []byte{7}}}
	block := NewBlock(header, txs, uncles, receipts, stubHasher{})
	blockExtras := &BlockBodyExtra{
		Version: 8,
		ExtData: ptrTo([]byte{9}),
	}
	allExportedFieldsSet(t, blockExtras) // make sure each field is checked
	extras.Block.Set(block, blockExtras)

	wantUncle := &Header{
		ParentHash: common.Hash{6},
		Difficulty: new(big.Int),
		Number:     new(big.Int),
	}
	// uncle returned from Body() has its extra set, even if the block uncle did not have it set
	SetHeaderExtra(wantUncle, &HeaderExtra{})
	wantBody := &Body{
		Transactions: []*Transaction{tx},
		Uncles:       []*Header{wantUncle},
	}
	wantBodyExtra := &BlockBodyExtra{
		Version: 8,
		ExtData: ptrTo([]byte{9}),
	}
	extras.Body.Set(wantBody, wantBodyExtra)

	body := block.Body()
	assert.Equal(t, wantBody, body)
	bodyExtra := extras.Body.Get(body)
	assert.Equal(t, wantBodyExtra, bodyExtra)

	exportedFieldsPointToDifferentMemory(t, blockExtras, bodyExtra)
}

func TestBlockGetters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		headerExtra        *HeaderExtra
		blockExtra         *BlockBodyExtra
		wantExtDataGasUsed *big.Int
		wantBlockGasCost   *big.Int
		wantVersion        uint32
		wantExtData        []byte
	}{
		{
			name:        "empty",
			headerExtra: &HeaderExtra{},
			blockExtra:  &BlockBodyExtra{},
		},
		{
			name: "fields_set",
			headerExtra: &HeaderExtra{
				ExtDataGasUsed: big.NewInt(1),
				BlockGasCost:   big.NewInt(2),
			},
			blockExtra: &BlockBodyExtra{
				Version: 3,
				ExtData: ptrTo([]byte{4}),
			},
			wantExtDataGasUsed: big.NewInt(1),
			wantBlockGasCost:   big.NewInt(2),
			wantVersion:        3,
			wantExtData:        []byte{4},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			header := &Header{}
			SetHeaderExtra(header, test.headerExtra)

			block := NewBlock(header, nil, nil, nil, stubHasher{})
			extras.Block.Set(block, test.blockExtra)

			extData := BlockExtData(block)
			assert.Equal(t, test.wantExtData, extData)

			version := BlockVersion(block)
			assert.Equal(t, test.wantVersion, version)

			extDataGasUsed := BlockExtDataGasUsed(block)
			assert.Equal(t, test.wantExtDataGasUsed, extDataGasUsed)

			blockGasCost := BlockGasCost(block)
			assert.Equal(t, test.wantBlockGasCost, blockGasCost)
		})
	}
}

func TestNewBlockWithExtData(t *testing.T) {
	t.Parallel()

	// This transaction is generated beforehand because of its unexported time field being set
	// on creation.
	testTx := NewTransaction(0, common.Address{1}, big.NewInt(2), 3, big.NewInt(4), []byte{5})

	tests := []struct {
		name      string
		header    func() *Header
		txs       []*Transaction
		uncles    func() []*Header
		receipts  []*Receipt
		extdata   []byte
		recalc    bool
		wantBlock func() *Block
	}{
		{
			name: "empty",
			header: func() *Header {
				header := &Header{}
				SetHeaderExtra(header, &HeaderExtra{})
				return header
			},
			wantBlock: func() *Block {
				header := &Header{}
				SetHeaderExtra(header, &HeaderExtra{})
				block := NewBlock(header, nil, nil, nil, stubHasher{})
				blockExtra := &BlockBodyExtra{ExtData: ptrTo([]byte{})}
				extras.Block.Set(block, blockExtra)
				return block
			},
		},
		{
			name: "with_recalc",
			header: func() *Header {
				header := &Header{}
				extra := &HeaderExtra{
					ExtDataHash: common.Hash{1}, // should be overwritten
				}
				SetHeaderExtra(header, extra)
				return header
			},
			extdata: []byte{2},
			recalc:  true,
			wantBlock: func() *Block {
				header := &Header{}
				extra := &HeaderExtra{
					ExtDataHash: common.HexToHash("0xf2ee15ea639b73fa3db9b34a245bdfa015c260c598b211bf05a1ecc4b3e3b4f2"),
				}
				SetHeaderExtra(header, extra)
				block := NewBlock(header, nil, nil, nil, stubHasher{})
				blockExtra := &BlockBodyExtra{ExtData: ptrTo([]byte{2})}
				extras.Block.Set(block, blockExtra)
				return block
			},
		},
		{
			name: "filled_no_recalc",
			header: func() *Header {
				header := &Header{GasLimit: 1}
				extra := &HeaderExtra{
					ExtDataHash:    common.Hash{2},
					ExtDataGasUsed: big.NewInt(3),
					BlockGasCost:   big.NewInt(4),
				}
				SetHeaderExtra(header, extra)
				return header
			},
			txs: []*Transaction{testTx},
			uncles: func() []*Header {
				uncle := &Header{GasLimit: 5}
				SetHeaderExtra(uncle, &HeaderExtra{BlockGasCost: big.NewInt(6)})
				return []*Header{uncle}
			},
			receipts: []*Receipt{{PostState: []byte{7}}},
			extdata:  []byte{8},
			wantBlock: func() *Block {
				header := &Header{GasLimit: 1}
				extra := &HeaderExtra{
					ExtDataHash:    common.Hash{2},
					ExtDataGasUsed: big.NewInt(3),
					BlockGasCost:   big.NewInt(4),
				}
				SetHeaderExtra(header, extra)
				uncle := &Header{GasLimit: 5}
				SetHeaderExtra(uncle, &HeaderExtra{BlockGasCost: big.NewInt(6)})
				uncles := []*Header{uncle}
				block := NewBlock(header, []*Transaction{testTx}, uncles, []*Receipt{{PostState: []byte{7}}}, stubHasher{})
				blockExtra := &BlockBodyExtra{ExtData: ptrTo([]byte{8})}
				extras.Block.Set(block, blockExtra)
				return block
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var uncles []*Header
			if test.uncles != nil {
				uncles = test.uncles()
			}

			block := NewBlockWithExtData(
				test.header(),
				test.txs,
				uncles,
				test.receipts,
				stubHasher{},
				test.extdata,
				test.recalc,
			)

			assert.Equal(t, test.wantBlock(), block)
		})
	}
}

type stubHasher struct{}

func (h stubHasher) Reset()                         {}
func (h stubHasher) Update(key, value []byte) error { return nil }
func (h stubHasher) Hash() common.Hash              { return common.Hash{} }
