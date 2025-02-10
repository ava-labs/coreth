// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/plugin/evm/ap3"
)

const DynamicFeeWindowSize = wrappers.LongLen * ap3.WindowLen

var ErrDynamicFeeWindowInsufficientLength = errors.New("insufficient length for dynamic fee window")

func ParseDynamicFeeWindow(bytes []byte) (ap3.Window, error) {
	if len(bytes) < DynamicFeeWindowSize {
		return ap3.Window{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeWindowInsufficientLength,
			DynamicFeeWindowSize,
			len(bytes),
		)
	}

	var window ap3.Window
	for i := range window {
		offset := i * wrappers.LongLen
		window[i] = binary.BigEndian.Uint64(bytes[offset:])
	}
	return window, nil
}

func DynamicFeeWindowBytes(w ap3.Window) []byte {
	bytes := make([]byte, DynamicFeeWindowSize)
	for i, v := range w {
		offset := i * wrappers.LongLen
		binary.BigEndian.PutUint64(bytes[offset:], v)
	}
	return bytes
}
