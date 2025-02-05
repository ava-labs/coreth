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

const ErrDynamicFeeAccumulatorSize = wrappers.LongLen * 3

var ErrDynamicFeeAccumulatorInsufficientLength = errors.New("insufficient length for dynamic fee accumulator")

type DynamicFeeAccumulator struct {
	GasState     gas.State
	TargetExcess gas.Gas
}

func ParseDynamicFeeAccumulator(bytes []byte) (DynamicFeeAccumulator, error) {
	if len(bytes) < ErrDynamicFeeAccumulatorSize {
		return DynamicFeeAccumulator{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeAccumulatorInsufficientLength,
			ErrDynamicFeeAccumulatorSize,
			len(bytes),
		)
	}

	return DynamicFeeAccumulator{
		GasState: gas.State{
			Capacity: gas.Gas(binary.BigEndian.Uint64(bytes)),
			Excess:   gas.Gas(binary.BigEndian.Uint64(bytes[wrappers.LongLen:])),
		},
		TargetExcess: gas.Gas(binary.BigEndian.Uint64(bytes[2*wrappers.LongLen:])),
	}, nil
}

func (d *DynamicFeeAccumulator) Bytes() []byte {
	bytes := make([]byte, ErrDynamicFeeAccumulatorSize)
	binary.BigEndian.PutUint64(bytes, uint64(d.GasState.Capacity))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(d.GasState.Excess))
	binary.BigEndian.PutUint64(bytes[2*wrappers.LongLen:], uint64(d.TargetExcess))
	return bytes
}
