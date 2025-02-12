// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderExtraGetWith(t *testing.T) {
	t.Parallel()

	h := &Header{}

	extra := GetHeaderExtra(h)
	require.NotNil(t, extra)
	assert.Equal(t, &HeaderExtra{}, extra)

	extra = &HeaderExtra{
		ExtDataHash: [32]byte{1},
	}
	WithHeaderExtra(h, extra)

	extra = GetHeaderExtra(h)
	assert.Equal(t, &HeaderExtra{
		ExtDataHash: [32]byte{1},
	}, extra)
}

func TestHeaderExtraRLP(t *testing.T) {
	t.Parallel()

	eth := &ethtypes.Header{
		ParentHash: common.Hash{1},
	}
	extra := &HeaderExtra{
		ExtDataHash: common.Hash{2},
	}

	writer := bytes.NewBuffer(nil)
	err := extra.EncodeRLP(eth, writer)
	require.NoError(t, err)

	stream := rlp.NewStream(bytes.NewReader(writer.Bytes()), 0)
	decodedExtra := new(HeaderExtra)
	decodedEth := new(ethtypes.Header)
	err = decodedExtra.DecodeRLP(decodedEth, stream)
	require.NoError(t, err)

	wantEth := &ethtypes.Header{
		ParentHash: common.Hash{1},
		Difficulty: new(big.Int),
		Number:     new(big.Int),
		Extra:      []byte{},
	}
	assert.Equal(t, wantEth, decodedEth)

	wantExtra := &HeaderExtra{
		ExtDataHash: common.Hash{2},
	}
	assert.Equal(t, wantExtra, decodedExtra)
}

func TestHeaderSerializable_updates(t *testing.T) {
	t.Parallel()

	t.Run("from", func(t *testing.T) {
		t.Parallel()

		eth := &ethtypes.Header{
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
		extras := &HeaderExtra{
			ExtDataHash:    common.Hash{21},
			ExtDataGasUsed: big.NewInt(22),
			BlockGasCost:   big.NewInt(23),
		}

		got := new(HeaderSerializable)
		got.updateFromEth(eth)
		got.updateFromExtras(extras)

		allFieldsAreSet(t, got)

		want := &HeaderSerializable{
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
			BlobGasUsed:      ptrTo(uint64(18)),
			ExcessBlobGas:    ptrTo(uint64(19)),
			ParentBeaconRoot: &common.Hash{20},
			ExtDataHash:      common.Hash{21},
			ExtDataGasUsed:   big.NewInt(22),
			BlockGasCost:     big.NewInt(23),
		}
		assert.Equal(t, want, got)
	})

	t.Run("to", func(t *testing.T) {
		t.Parallel()

		serializable := &HeaderSerializable{
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
			BlobGasUsed:      ptrTo(uint64(18)),
			ExcessBlobGas:    ptrTo(uint64(19)),
			ParentBeaconRoot: &common.Hash{20},
			ExtDataHash:      common.Hash{21},
			ExtDataGasUsed:   big.NewInt(22),
			BlockGasCost:     big.NewInt(23),
		}
		allFieldsAreSet(t, serializable)

		eth := new(ethtypes.Header)
		extras := new(HeaderExtra)

		serializable.updateToEth(eth)
		serializable.updateToExtras(extras)

		// Note eth isn't checked for all its fields to be set
		// since some of its fields may not be required to be set,
		// such as WithdrawalsHash.
		allFieldsAreSet(t, extras)

		wantedEth := &ethtypes.Header{
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
			BlobGasUsed:      ptrTo(uint64(18)),
			ExcessBlobGas:    ptrTo(uint64(19)),
			ParentBeaconRoot: &common.Hash{20},
		}
		wantedExtras := &HeaderExtra{
			ExtDataHash:    common.Hash{21},
			ExtDataGasUsed: big.NewInt(22),
			BlockGasCost:   big.NewInt(23),
		}

		assert.Equal(t, wantedEth, eth)
		assert.Equal(t, wantedExtras, extras)
	})
}
