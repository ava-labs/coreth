// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"math/big"
	"reflect"
	"testing"
	"unicode"
	"unsafe"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/pseudo"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyHeader(t *testing.T) {
	t.Run("empty_header", func(t *testing.T) {
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
		h := &Header{
			ParentHash:       common.Hash{1},
			UncleHash:        common.Hash{2},
			Coinbase:         common.Address{3},
			Root:             common.Hash{4},
			TxHash:           common.Hash{5},
			ReceiptHash:      common.Hash{6},
			Bloom:            Bloom{7},
			Difficulty:       big.NewInt(8),
			Number:           big.NewInt(9),
			GasLimit:         10,
			GasUsed:          11,
			Time:             12,
			Extra:            []byte{13},
			MixDigest:        common.Hash{14},
			Nonce:            BlockNonce{15},
			BaseFee:          big.NewInt(16),
			WithdrawalsHash:  &common.Hash{17},
			BlobGasUsed:      ptrTo(uint64(18)),
			ExcessBlobGas:    ptrTo(uint64(19)),
			ParentBeaconRoot: &common.Hash{20},
		}
		headerExtra := &HeaderExtra{
			ExtDataHash:    common.Hash{21},
			ExtDataGasUsed: big.NewInt(22),
			BlockGasCost:   big.NewInt(23),
		}
		extras.Header.Set(h, headerExtra)

		allFieldsAreSet(t, h)
		allFieldsAreSet(t, headerExtra)

		cpy := CopyHeader(h)

		want := &Header{
			ParentHash:       common.Hash{1},
			UncleHash:        common.Hash{2},
			Coinbase:         common.Address{3},
			Root:             common.Hash{4},
			TxHash:           common.Hash{5},
			ReceiptHash:      common.Hash{6},
			Bloom:            Bloom{7},
			Difficulty:       big.NewInt(8),
			Number:           big.NewInt(9),
			GasLimit:         10,
			GasUsed:          11,
			Time:             12,
			Extra:            []byte{13},
			MixDigest:        common.Hash{14},
			Nonce:            BlockNonce{15},
			BaseFee:          big.NewInt(16),
			WithdrawalsHash:  &common.Hash{17},
			BlobGasUsed:      ptrTo(uint64(18)),
			ExcessBlobGas:    ptrTo(uint64(19)),
			ParentBeaconRoot: &common.Hash{20},
		}
		headerExtra = &HeaderExtra{
			ExtDataHash:    common.Hash{21},
			ExtDataGasUsed: big.NewInt(22),
			BlockGasCost:   big.NewInt(23),
		}
		extras.Header.Set(want, headerExtra)

		assert.Equal(t, want, cpy)

		// Mutate each non-value field to ensure they are not shared
		fieldsAreDeepCopied(t, h, cpy)
		fieldsAreDeepCopied(t, GetHeaderExtra(h), GetHeaderExtra(cpy))
	})
}

func ptrTo[T any](x T) *T { return &x }

func allFieldsAreSet(t *testing.T, x any) {
	t.Helper()
	require.Equal(t, reflect.Ptr.String(), reflect.TypeOf(x).Kind().String(), "x must be a pointer")
	v := reflect.ValueOf(x).Elem()
	typ := v.Type()
	require.Equal(t, reflect.Struct.String(), typ.Kind().String())
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := typ.Field(i).Name
		fieldValue := field
		if unicode.IsLower(rune(fieldName[0])) { // unexported
			if field.Kind() == reflect.Ptr {
				require.Falsef(t, field.IsNil(), "field %q is nil", fieldName)
			}
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem() //nolint:gosec
			fieldValue = field
		}
		if field.Kind() == reflect.Pointer {
			require.NotNilf(t, field.Interface(), "field %q is nil", fieldName)
			fieldValue = field.Elem()
		}
		isSet := fieldValue.IsValid() && !fieldValue.IsZero()
		require.True(t, isSet, "field %q is not set", fieldName)
	}
}

func fieldsAreDeepCopied(t *testing.T, original, cpy any) {
	t.Helper()
	require.Equal(t, reflect.Ptr.String(), reflect.TypeOf(original).Kind().String(), "original must be a pointer")
	require.Equal(t, reflect.Ptr.String(), reflect.TypeOf(cpy).Kind().String(), "cpy must be a pointer")

	v := reflect.ValueOf(original).Elem()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := v.Type().Field(i).Name
		isUnexported := unicode.IsLower(rune(fieldName[0]))
		if isUnexported {
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem() //nolint:gosec
		}
		var originalField any
		switch field.Kind() {
		case reflect.Array, reflect.Uint32, reflect.Uint64: // values
		case reflect.Slice:
			originalField = field.Interface()
			switch originalField.(type) {
			case []byte:
				field.Set(reflect.Append(field, reflect.ValueOf(byte(1))))
			default:
				t.Fatalf("unexpected slice type %T for %q", originalField, fieldName)
			}
			originalField = field.Interface()
		case reflect.Pointer:
			originalField = field.Interface()
			switch ptr := originalField.(type) {
			case *big.Int:
				ptr.Add(ptr, big.NewInt(1))
			case *uint64:
				*ptr++
			case *common.Hash:
				ptr[0]++
			case *[]byte:
				*ptr = append(*ptr, 1)
				(*ptr)[0]++
			case *pseudo.Type:
				continue // extras are checked separately in another call of [fieldsAreDeepCopied]
			default:
				t.Fatalf("unexpected pointer type %T for %q", ptr, fieldName)
			}
		default:
			t.Fatalf("unexpected field kind %v for %q", field.Kind(), fieldName)
		}

		cpyField := reflect.ValueOf(cpy).Elem().Field(i)
		if isUnexported {
			cpyField = reflect.NewAt(cpyField.Type(), unsafe.Pointer(cpyField.UnsafeAddr())).Elem() //nolint:gosec
		}
		assert.NotEqualf(t, originalField, cpyField.Interface(), "field %q", fieldName)
	}
}

func TestBlockExtraGetWith(t *testing.T) {
	t.Parallel()

	b := &Block{}

	extra := GetBlockExtra(b)
	require.NotNil(t, extra)
	assert.Equal(t, &BlockBodyExtra{}, extra)

	extra = &BlockBodyExtra{
		Version: 1,
	}
	WithBlockExtra(b, extra)

	extra = GetBlockExtra(b)
	wantExtra := &BlockBodyExtra{
		Version: 1,
	}
	assert.Equal(t, wantExtra, extra)
}

func TestBodyExtraGetWith(t *testing.T) {
	t.Parallel()

	b := &Body{}

	extra := GetBodyExtra(b)
	require.NotNil(t, extra)
	assert.Equal(t, &BlockBodyExtra{}, extra)

	extra = &BlockBodyExtra{
		Version: 1,
	}
	WithBodyExtra(b, extra)

	extra = GetBodyExtra(b)
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
	_ = WithHeaderExtra(uncle, uncleExtra)

	body := &Body{
		Transactions: []*Transaction{testTx},
		Uncles:       []*Header{uncle},
		Withdrawals:  []*ethtypes.Withdrawal{{Index: 7}}, // ignored
	}
	extra := &BlockBodyExtra{
		Version: 1,
		ExtData: ptrTo([]byte{2}),
	}
	_ = WithBodyExtra(body, extra)

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
	_ = WithHeaderExtra(wantUncle, wantUncleExtra)

	wantBody := &Body{
		Transactions: nil, // checked above
		Uncles:       []*Header{wantUncle},
	}
	wantExtra := &BlockBodyExtra{
		Version: 1,
		ExtData: ptrTo([]byte{2}),
	}
	_ = WithBodyExtra(wantBody, wantExtra)

	assert.Equal(t, wantBody, gotBody)
	assert.Equal(t, wantExtra, GetBodyExtra(gotBody))
}

func TestBlockExtraRLP(t *testing.T) {
	t.Parallel()

	header := &Header{ParentHash: common.Hash{1}}
	headerExtras := &HeaderExtra{ExtDataHash: common.Hash{2}}
	_ = WithHeaderExtra(header, headerExtras)

	testTx := NewTransaction(3, common.Address{4}, big.NewInt(2), 3, big.NewInt(4), []byte{5})
	txs := []*Transaction{testTx}

	uncle := &Header{ParentHash: common.Hash{6}}
	uncleExtra := &HeaderExtra{ExtDataHash: common.Hash{7}}
	_ = WithHeaderExtra(uncle, uncleExtra)
	uncles := []*Header{uncle}

	receipts := []*Receipt{{PostState: []byte{8}}}

	block := NewBlock(header, txs, uncles, receipts, stubHasher{})

	withdrawals := []*ethtypes.Withdrawal{{Index: 9}} // ignored
	block = block.WithWithdrawals(withdrawals)

	extra := &BlockBodyExtra{
		Version: 10,
		ExtData: ptrTo([]byte{11}),
	}
	_ = WithBlockExtra(block, extra)

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
	_ = WithHeaderExtra(wantHeader, wantHeaderExtras)

	wantUncle := &Header{
		ParentHash: common.Hash{6},
		Difficulty: new(big.Int), // field set by RLP decoder
		Number:     new(big.Int), // field set by RLP decoder
		Extra:      []byte{},     // field set by RLP decoder
	}
	wantUncleExtra := &HeaderExtra{ExtDataHash: common.Hash{7}}
	_ = WithHeaderExtra(wantUncle, wantUncleExtra)
	wantUncles := []*Header{wantUncle}

	wantBlock := NewBlock(wantHeader, gotTxs /* checked above */, wantUncles, receipts, stubHasher{})
	wantExtra := &BlockBodyExtra{
		Version: 10,
		ExtData: ptrTo([]byte{11}),
	}
	_ = WithBlockExtra(wantBlock, wantExtra)
	_ = wantBlock.Size() // set block cached unexported size field

	assert.Equal(t, wantBlock, gotBlock)
	assert.Equal(t, wantExtra, GetBlockExtra(gotBlock))
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
	extras := &BlockBodyExtra{
		Version: 8,
		ExtData: ptrTo([]byte{9}),
	}
	allFieldsAreSet(t, extras) // make sure each field is checked
	_ = WithBlockExtra(block, extras)

	wantUncle := &Header{
		ParentHash: common.Hash{6},
		Difficulty: new(big.Int),
		Number:     new(big.Int),
	}
	// uncle returned from Body() has its extra set, even if the block uncle did not have it set
	_ = WithHeaderExtra(wantUncle, &HeaderExtra{})
	wantBody := &Body{
		Transactions: []*Transaction{tx},
		Uncles:       []*Header{wantUncle},
	}
	wantBodyExtra := &BlockBodyExtra{
		Version: 8,
		ExtData: ptrTo([]byte{9}),
	}
	_ = WithBodyExtra(wantBody, wantBodyExtra)

	body := block.Body()
	assert.Equal(t, wantBody, body)
	bodyExtra := GetBodyExtra(body)
	assert.Equal(t, wantBodyExtra, bodyExtra)

	fieldsAreDeepCopied(t, extras, bodyExtra)
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
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			header := &Header{}
			_ = WithHeaderExtra(header, test.headerExtra)

			block := NewBlock(header, nil, nil, nil, stubHasher{})
			_ = WithBlockExtra(block, test.blockExtra)

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
		header    *Header
		txs       []*Transaction
		uncles    []*Header
		receipts  []*Receipt
		extdata   []byte
		recalc    bool
		wantBlock *Block
	}{
		{
			name:   "empty",
			header: WithHeaderExtra(&Header{}, &HeaderExtra{}),
			wantBlock: WithBlockExtra(
				NewBlock(WithHeaderExtra(&Header{}, &HeaderExtra{}), nil, nil, nil, stubHasher{}),
				&BlockBodyExtra{ExtData: ptrTo([]byte{})},
			),
		},
		{
			name: "with_recalc",
			header: WithHeaderExtra(
				&Header{},
				&HeaderExtra{
					ExtDataHash: common.Hash{1}, // should be overwritten
				},
			),
			extdata: []byte{2},
			recalc:  true,
			wantBlock: WithBlockExtra(
				NewBlock(
					WithHeaderExtra(
						&Header{},
						&HeaderExtra{
							ExtDataHash: common.HexToHash("0xf2ee15ea639b73fa3db9b34a245bdfa015c260c598b211bf05a1ecc4b3e3b4f2"),
						},
					), nil, nil, nil, stubHasher{}),
				&BlockBodyExtra{ExtData: ptrTo([]byte{2})},
			),
		},
		{
			name: "filled_no_recalc",
			header: WithHeaderExtra(
				&Header{GasLimit: 1},
				&HeaderExtra{
					ExtDataHash:    common.Hash{2},
					ExtDataGasUsed: big.NewInt(3),
					BlockGasCost:   big.NewInt(4),
				},
			),
			txs:      []*Transaction{testTx},
			uncles:   []*Header{WithHeaderExtra(&Header{GasLimit: 5}, &HeaderExtra{BlockGasCost: big.NewInt(6)})},
			receipts: []*Receipt{{PostState: []byte{7}}},
			extdata:  []byte{8},
			wantBlock: WithBlockExtra(
				NewBlock(
					WithHeaderExtra(
						&Header{GasLimit: 1},
						&HeaderExtra{
							ExtDataHash:    common.Hash{2},
							ExtDataGasUsed: big.NewInt(3),
							BlockGasCost:   big.NewInt(4),
						},
					),
					[]*Transaction{testTx},
					[]*Header{WithHeaderExtra(&Header{GasLimit: 5}, &HeaderExtra{BlockGasCost: big.NewInt(6)})},
					[]*Receipt{{PostState: []byte{7}}},
					stubHasher{}),
				&BlockBodyExtra{ExtData: ptrTo([]byte{8})},
			),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			block := NewBlockWithExtData(
				test.header,
				test.txs,
				test.uncles,
				test.receipts,
				stubHasher{},
				test.extdata,
				test.recalc,
			)

			assert.Equal(t, test.wantBlock, block)
		})
	}
}

type stubHasher struct{}

func (h stubHasher) Reset()                         {}
func (h stubHasher) Update(key, value []byte) error { return nil }
func (h stubHasher) Hash() common.Hash              { return common.Hash{} }
