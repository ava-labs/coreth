// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/plugin/evm/acp176"
)

const DynamicFeeAccumulatorSize = wrappers.LongLen * 2

var ErrDynamicFeeAccumulatorInsufficientLength = errors.New("insufficient length for dynamic fee accumulator")

func ParseDynamicFeeAccumulator(
	limit uint64,
	gasUsed uint64,
	extraGasUsed *big.Int,
	bytes []byte,
) (acp176.State, error) {
	if len(bytes) < DynamicFeeAccumulatorSize {
		return acp176.State{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeAccumulatorInsufficientLength,
			DynamicFeeAccumulatorSize,
			len(bytes),
		)
	}

	capacity, err := math.Sub(limit, gasUsed)
	if err != nil {
		return acp176.State{}, err
	}
	if extraGasUsed != nil {
		capacity, err = math.Sub(capacity, extraGasUsed.Uint64())
	}

	return acp176.State{
		Gas: gas.State{
			Capacity: gas.Gas(capacity),
			Excess:   gas.Gas(binary.BigEndian.Uint64(bytes)),
		},
		TargetExcess: gas.Gas(binary.BigEndian.Uint64(bytes[wrappers.LongLen:])),
	}, err
}

func DynamicFeeAccumulatorBytes(s acp176.State) []byte {
	bytes := make([]byte, DynamicFeeAccumulatorSize)
	binary.BigEndian.PutUint64(bytes, uint64(s.Gas.Excess))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(s.TargetExcess))
	return bytes
}
