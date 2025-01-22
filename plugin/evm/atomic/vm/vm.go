package vm

import (
	"context"
	"fmt"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/coreth/plugin/evm/atomic/sync"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/message"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ secp256k1fx.VM                     = (*VM)(nil)
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
	_ block.StateSyncableVM              = (*VM)(nil)
)

type InnerVM interface {
	avalanchecommon.VM
	secp256k1fx.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

type VM struct {
	InnerVM
}

func NewAtomicExtensionConfig() (extension.ExtensionConfig, error) {
	codec, err := message.NewCodec(sync.AtomicSyncSummary{})
	if err != nil {
		return extension.ExtensionConfig{}, fmt.Errorf("failed to create codec manager: %w", err)
	}
	return extension.ExtensionConfig{
		NetworkCodec: codec,
	}, nil
}

func WrapVM(vm InnerVM) *VM {
	return &VM{InnerVM: vm}
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db avalanchedatabase.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- avalanchecommon.Message,
	fxs []*avalanchecommon.Fx,
	appSender avalanchecommon.AppSender,
) error {
	return vm.InnerVM.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		toEngine,
		fxs,
		appSender,
	)
}
