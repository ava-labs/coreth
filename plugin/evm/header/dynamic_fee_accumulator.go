// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

const DynamicFeeStateSize = wrappers.LongLen * 2

var ErrDynamicFeeStateInsufficientLength = errors.New("insufficient length for dynamic fee state")

func ParseDynamicFeeState(bytes []byte) (gas.State, error) {
	if len(bytes) < DynamicFeeWindowSize {
		return gas.State{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeStateInsufficientLength,
			DynamicFeeStateSize,
			len(bytes),
		)
	}

	return gas.State{
		Capacity: gas.Gas(binary.BigEndian.Uint64(bytes)),
		Excess:   gas.Gas(binary.BigEndian.Uint64(bytes[wrappers.LongLen:])),
	}, nil
}

func DynamicFeeStateBytes(s gas.State) []byte {
	bytes := make([]byte, DynamicFeeStateSize)
	binary.BigEndian.PutUint64(bytes, uint64(s.Capacity))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(s.Excess))
	return bytes
}
