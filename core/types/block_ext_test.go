// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"math/big"
	"reflect"
	"testing"
	"unsafe"

	"github.com/ava-labs/libevm/common"
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
		if !typ.Field(i).IsExported() {
			if field.Kind() == reflect.Ptr {
				require.Falsef(t, field.IsNil(), "field %q is nil", fieldName)
			}
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem() //nolint:gosec
			fieldValue = field
		}
		if field.Kind() == reflect.Pointer {
			require.Falsef(t, field.IsNil(), "field %q is nil", fieldName)
			fieldValue = field.Elem()
		}
		require.False(t, fieldValue.IsZero(), "field %q is not set", fieldName)
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
		isUnexported := !v.Type().Field(i).IsExported()
		if isUnexported {
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem() //nolint:gosec
		}
		var originalField any
		switch field.Kind() {
		case reflect.Array, reflect.Uint64: // values
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
			if field.Type().String() == "*pseudo.Type" {
				// extras are checked separately in another call of [fieldsAreDeepCopied]
				// Use the string type to prevent importing libevm/pseudo package.
				continue
			}
			switch ptr := originalField.(type) {
			case *big.Int:
				ptr.Add(ptr, big.NewInt(1))
			case *uint64:
				*ptr++
			case *common.Hash:
				ptr[0]++
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
