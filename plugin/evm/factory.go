// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"

	atomicvm "github.com/ava-labs/coreth/plugin/evm/atomic/vm"
)

var (
	// ID this VM should be referenced by
	ID = ids.ID{'e', 'v', 'm'}

	_ vms.Factory = &Factory{}
)

type Factory struct{}

func (*Factory) New(logging.Logger) (interface{}, error) {
	extensionCfg, err := atomicvm.NewAtomicExtensionConfig()
	if err != nil {
		return nil, err
	}
	return atomicvm.WrapVM(NewExtensibleEVM(false, extensionCfg)), nil
}

func NewPluginVM() (block.ChainVM, error) {
	extensionCfg, err := atomicvm.NewAtomicExtensionConfig()
	if err != nil {
		return nil, err
	}
	return atomicvm.WrapVM(NewExtensibleEVM(true, extensionCfg)), nil
}
