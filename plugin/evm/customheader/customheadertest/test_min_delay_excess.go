// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheadertest

import "github.com/ava-labs/avalanchego/vms/evm/acp226"

func NewDelayExcessPtr(value uint64) *acp226.DelayExcess {
	delayExcess := acp226.DelayExcess(value)
	return &delayExcess
}
