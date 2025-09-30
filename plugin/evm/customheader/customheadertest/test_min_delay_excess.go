package customheadertest

import "github.com/ava-labs/avalanchego/vms/evm/acp226"

func NewDelayExcessPtr(value uint64) *acp226.DelayExcess {
	delayExcess := acp226.DelayExcess(value)
	return &delayExcess
}
