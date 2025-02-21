// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrTo[T any](x T) *T { return &x }

// headerWithNonZeroFields returns a Header and extra payload, each with all
// fields set to non-zero values. The Header also has the payload added to it
// via [SetHeaderExtra].
//
// NOTE: They can be used to demonstrate that RLP and JSON round-trip encoding
// can recover all fields, but not that the encoded format is correct. This is
// very important as the RLP encoding of a Header defines its hash.
func headerWithNonZeroFields() (*ethtypes.Header, *HeaderExtra) {
	hdr := &ethtypes.Header{
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
	extra := &HeaderExtra{
		ExtDataHash:    common.Hash{21},
		ExtDataGasUsed: big.NewInt(22),
		BlockGasCost:   big.NewInt(23),
	}
	SetHeaderExtra(hdr, extra)
	return hdr, extra
}

func TestHeaderRLP(t *testing.T) {
	testHeaderEncodings(t, rlp.EncodeToBytes, rlp.DecodeBytes)
}

func TestHeaderJSON(t *testing.T) {
	testHeaderEncodings(t, json.Marshal, json.Unmarshal)
}

func testHeaderEncodings(t *testing.T, encode func(any) ([]byte, error), decode func([]byte, any) error) {
	t.Helper()

	hdr, _ := headerWithNonZeroFields() // the Header carries the HeaderExtra so we can ignore it
	buf, err := encode(hdr)
	require.NoError(t, err)

	gotHdr := new(Header)
	err = decode(buf, gotHdr)
	require.NoError(t, err)
	gotExtra := GetHeaderExtra(gotHdr)

	wantHdr, wantExtra := headerWithNonZeroFields()
	wantHdr.WithdrawalsHash = nil
	assert.Equal(t, wantHdr, gotHdr)
	assert.Equal(t, wantExtra, gotExtra)
}

func TestHeaderWithNonZeroFields(t *testing.T) {
	hdr, extra := headerWithNonZeroFields()
	t.Run("Header", func(t *testing.T) { allFieldsNonZero(t, hdr) })
	t.Run("HeaderExtra", func(t *testing.T) { allFieldsNonZero(t, extra) })
}

func allFieldsNonZero[T interface {
	ethtypes.Header | HeaderExtra
}](t *testing.T, x *T) {
	// We don't test for nil pointers because we're only confirming that
	// test-input data is well-formed. A panic due to a dereference will be
	// reported anyway.

	v := reflect.ValueOf(*x)
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		fld := typ.Field(i)
		if !fld.IsExported() {
			continue
		}

		t.Run(fld.Name, func(t *testing.T) {
			switch f := v.Field(i).Interface().(type) {
			case *big.Int:
				assert.NotEqual(t, 0, f.Cmp(big.NewInt(0)))
			case common.Hash:
				assertNonZero(t, f)
			case *common.Hash:
				assertNonZero(t, *f)
			case common.Address:
				assertNonZero(t, f)
			case BlockNonce:
				assertNonZero(t, f)
			case Bloom:
				assertNonZero(t, f)
			case uint64:
				assertNonZero(t, f)
			case *uint64:
				assertNonZero(t, *f)
			case []uint8:
				assert.NotEmpty(t, f)
			default:
				t.Errorf("Field %q has unsupported type %T", fld.Name, f)
			}
		})
	}
}

func assertNonZero[T interface {
	common.Hash | common.Address | BlockNonce | uint64 | Bloom
}](t *testing.T, v T) {
	t.Helper()
	var zero T
	assert.NotEqualf(t, zero, v, "must not be zero value for %T", v)
}
