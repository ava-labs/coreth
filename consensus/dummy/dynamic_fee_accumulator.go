// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ethereum/go-ethereum/common"
)

func CalcACP176GasState(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (header.DynamicFeeAccumulator, error) {
	if timestamp < parent.Time {
		return header.DynamicFeeAccumulator{}, fmt.Errorf("cannot calculate gas state for timestamp %d prior to parent timestamp %d",
			timestamp,
			parent.Time,
		)
	}

	var gasAccumulator header.DynamicFeeAccumulator
	if config.IsFUpgrade(parent.Time) && parent.Number.Cmp(common.Big0) != 0 {
		// If the parent block was running with ACP-176, we start with the
		// parent's fee state.
		var err error
		gasAccumulator, err = header.ParseDynamicFeeAccumulator(
			parent.GasLimit,
			parent.GasUsed,
			parent.ExtDataGasUsed,
			parent.Extra,
		)
		if err != nil {
			return header.DynamicFeeAccumulator{}, err
		}
	}

	gasAccumulator.AdvanceTime(timestamp - parent.Time)
	return gasAccumulator, nil
}
