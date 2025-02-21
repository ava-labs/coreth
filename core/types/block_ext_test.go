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
		hdr, _ := headerWithNonZeroFields() // the Header carries the HeaderExtra so we can ignore it

		gotHdr := CopyHeader(hdr)
		gotExtra := GetHeaderExtra(gotHdr)
		wantHdr, wantExtra := headerWithNonZeroFields()

		assert.Equal(t, wantHdr, gotHdr)
		assert.Equal(t, wantExtra, gotExtra)

		fieldsPointToDifferentMemory(t, hdr, gotHdr)
		fieldsPointToDifferentMemory(t, GetHeaderExtra(hdr), gotExtra)
	})
}

func fieldsPointToDifferentMemory[T interface {
	Header | HeaderExtra
}](t *testing.T, original, cpy *T) {
	t.Helper()

	v := reflect.ValueOf(*original)
	typ := v.Type()
	cp := reflect.ValueOf(*cpy)
	for i := 0; i < v.NumField(); i++ {
		fld := typ.Field(i)
		if !fld.IsExported() {
			continue
		}

		t.Run(fld.Name, func(t *testing.T) {
			switch fld.Type.Kind() {
			case reflect.Array, reflect.Uint64:
				// Not pointers, but using explicit Kinds for safety
				return
			}

			fldCopy := cp.Field(i).Interface()
			switch f := v.Field(i).Interface().(type) {
			case *big.Int:
				assertDifferentPointers(t, f, fldCopy)
			case *common.Hash:
				assertDifferentPointers(t, f, fldCopy)
			case *uint64:
				assertDifferentPointers(t, f, fldCopy)
			case []uint8:
				assertDifferentPointers(t, unsafe.SliceData(f), unsafe.SliceData(fldCopy.([]uint8)))
			default:
				t.Errorf("Field %q has unsupported type %T", fld.Name, f)
			}
		})
	}
}

// assertDifferentPointers asserts that `a` and `b` point to different memory
// locations or that both are nil `b` MUST be of the same type as `a`.
func assertDifferentPointers[T any](t *testing.T, a *T, b any) {
	t.Helper()
	switch b := b.(type) {
	case *T:
		if a != nil && a == b {
			t.Error("pointers to same memory")
		}
	default:
		t.Errorf("BUG IN TEST: comparing pointers of type %T and %T", a, b)
	}
}
