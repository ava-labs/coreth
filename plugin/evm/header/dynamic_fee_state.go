// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"

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
		// resulting fee state from the parent block. It is assumed that the
		// parent has been verified, so the claimed fee state equals the actual
		// fee state.
		var err error
		state, err = claimedFeeStateAfterBlock(parent)
		if err != nil {
			return acp176.State{}, err
		}
	}

	state.AdvanceTime(timestamp - parent.Time)
	return state, nil
}

// feeStateAfterBlock takes the previous header and returns the fee state after
// the execution of the provided child.
//
// This function does not clamp the gas capacity to be within the maximum
// capacity. The caller must either manually clamp the capacity or advance the
// time of the state to clamp the capacity.
func feeStateAfterBlock(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
	desiredTargetExcess *gas.Gas,
) (acp176.State, error) {
	// Calculate the gas state after the parent block
	state, err := feeStateBeforeBlock(config, parent, header.Time)
	if err != nil {
		return acp176.State{}, err
	}

	// Consume the gas used by the block
	if err := state.ConsumeGas(header.GasUsed, header.ExtDataGasUsed); err != nil {
		return acp176.State{}, err
	}

	// If the desired target excess is specified, move the target excess as much
	// as possible toward that desired value.
	if desiredTargetExcess != nil {
		state.UpdateTargetExcess(*desiredTargetExcess)
	}
	return state, nil
}

// claimedFeeStateAfterBlock returns the unverified fee state encoded in a
// header.
//
// This function does not clamp the gas capacity to be within the maximum
// capacity. The caller must either manually clamp the capacity or advance the
// time of the state to clamp the capacity.
func claimedFeeStateAfterBlock(header *types.Header) (acp176.State, error) {
	if len(header.Extra) < FeeExcessSize {
		return acp176.State{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			errFeeExcessInsufficientLength,
			FeeExcessSize,
			len(header.Extra),
		)
	}

	capacity, err := math.Sub(header.GasLimit, header.GasUsed)
	if err != nil {
		return acp176.State{}, err
	}
	if header.ExtDataGasUsed != nil {
		capacity, err = math.Sub(capacity, header.ExtDataGasUsed.Uint64())
		if err != nil {
			return acp176.State{}, err
		}
	}

	return acp176.State{
		Gas: gas.State{
			Capacity: gas.Gas(capacity),
			Excess:   gas.Gas(binary.BigEndian.Uint64(header.Extra)),
		},
		TargetExcess: gas.Gas(binary.BigEndian.Uint64(header.Extra[wrappers.LongLen:])),
	}, nil
}

func feeExcessBytes(s acp176.State) []byte {
	bytes := make([]byte, FeeExcessSize)
	binary.BigEndian.PutUint64(bytes, uint64(s.Gas.Excess))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(s.TargetExcess))
	return bytes
}
