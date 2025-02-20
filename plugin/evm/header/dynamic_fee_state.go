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
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ethereum/go-ethereum/common"
)

const FeeExcessSize = wrappers.LongLen * 2

var errFeeExcessInsufficientLength = errors.New("insufficient length for dynamic fee excess")

// feeStateBeforeBlock takes the previous header and the timestamp of its child
// block and calculates the fee state before the child block is executed.
func feeStateBeforeBlock(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (acp176.State, error) {
	if timestamp < parent.Time {
		return acp176.State{}, fmt.Errorf("cannot calculate gas state for timestamp %d prior to parent timestamp %d",
			timestamp,
			parent.Time,
		)
	}

	var state acp176.State
	if config.IsFUpgrade(parent.Time) && parent.Number.Cmp(common.Big0) != 0 {
		// If the parent block was running with ACP-176, we start with the
		// resulting fee state from the parent block.
		var err error
		state, err = feeStateAfterBlock(
			parent.GasLimit,
			parent.GasUsed,
			parent.ExtDataGasUsed,
			parent.Extra,
		)
		if err != nil {
			return acp176.State{}, err
		}
	}

	state.AdvanceTime(timestamp - parent.Time)
	return state, nil
}

// feeStateAfterBlock returns the fee state after the execution of a block whose
// header include the given fields.
//
// This function does not clamp the gas capacity to be within the maximum
// capacity. The caller must either manually clamp the capacity or advance the
// time of the state to clamp the capacity.
func feeStateAfterBlock(
	limit uint64,
	gasUsed uint64,
	extraGasUsed *big.Int,
	extra []byte,
) (acp176.State, error) {
	if len(extra) < FeeExcessSize {
		return acp176.State{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			errFeeExcessInsufficientLength,
			FeeExcessSize,
			len(extra),
		)
	}

	capacity, err := math.Sub(limit, gasUsed)
	if err != nil {
		return acp176.State{}, err
	}
	if extraGasUsed != nil {
		capacity, err = math.Sub(capacity, extraGasUsed.Uint64())
		if err != nil {
			return acp176.State{}, err
		}
	}

	return acp176.State{
		Gas: gas.State{
			Capacity: gas.Gas(capacity),
			Excess:   gas.Gas(binary.BigEndian.Uint64(extra)),
		},
		TargetExcess: gas.Gas(binary.BigEndian.Uint64(extra[wrappers.LongLen:])),
	}, nil
}

func feeExcessBytes(s acp176.State) []byte {
	bytes := make([]byte, FeeExcessSize)
	binary.BigEndian.PutUint64(bytes, uint64(s.Gas.Excess))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(s.TargetExcess))
	return bytes
}
