// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common/math"
)

var ErrInsufficientDynamicFeeWindowLength = errors.New("insufficient length for dynamic fee window")

type DynamicFeeWindow [params.RollupWindow]uint64

func ParseDynamicFeeWindow(bytes []byte) (DynamicFeeWindow, error) {
	if len(bytes) < params.DynamicFeeExtraDataSize {
		return DynamicFeeWindow{}, fmt.Errorf("%w expected at least %d, but found %d",
			ErrInsufficientDynamicFeeWindowLength,
			params.DynamicFeeExtraDataSize,
			len(bytes),
		)
	}

	var window DynamicFeeWindow
	for i := range window {
		offset := i * wrappers.LongLen
		window[i] = binary.BigEndian.Uint64(bytes[offset:])
	}
	return window, nil
}

// Add adds the amount to the most recent entry in the window.
//
// If the most recent entry overflows, it is set to [math.MaxUint64].
func (w *DynamicFeeWindow) Add(amount uint64) {
	const lastIndex uint = params.RollupWindow - 1
	(*w)[lastIndex] = add(w[lastIndex], amount)
}

// Shift removes the oldest amount entries from the window and adds amount new
// empty entries.
func (w *DynamicFeeWindow) Shift(amount uint64) {
	if amount >= params.RollupWindow {
		*w = DynamicFeeWindow{}
		return
	}

	var newWindow DynamicFeeWindow
	copy(newWindow[:], w[amount:])
	*w = newWindow
}

// Sum returns the sum of all the entries in the window.
//
// If the sum overflows, [math.MaxUint64] is returned.
func (w *DynamicFeeWindow) Sum() uint64 {
	var sum uint64
	for _, v := range w {
		sum = add(sum, v)
	}
	return sum
}

func (w *DynamicFeeWindow) Bytes() []byte {
	bytes := make([]byte, len(w)*wrappers.LongLen)
	for i, v := range w {
		offset := i * wrappers.LongLen
		binary.BigEndian.PutUint64(bytes[offset:], v)
	}
	return bytes
}

func add(sum uint64, values ...uint64) uint64 {
	var overflow bool
	for _, v := range values {
		sum, overflow = math.SafeAdd(sum, v)
		if overflow {
			return math.MaxUint64
		}
	}
	return sum
}
