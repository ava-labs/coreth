// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/proposervm"
)

var (
	// ID this VM should be referenced by
	ID = ids.ID{'e', 'v', 'm'}

	_ vms.Factory = &Factory{}
)

type FactoryConfig struct {
	ProposerVMConfig proposervm.Config
}

type Factory struct {
	FactoryConfig FactoryConfig
}

func (f *Factory) New(logging.Logger) (interface{}, error) {
	return proposervm.New(&VM{}, f.FactoryConfig.ProposerVMConfig), nil
}
