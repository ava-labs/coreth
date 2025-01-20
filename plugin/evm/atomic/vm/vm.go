package vm

import (
	"context"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ secp256k1fx.VM                     = &VM{}
	_ block.ChainVM                      = &VM{}
	_ block.BuildBlockWithContextChainVM = &VM{}
	_ block.StateSyncableVM              = &VM{}
)

type innerVM interface {
	avalanchecommon.VM
	secp256k1fx.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

type VM struct {
	innerVM // Inner EVM
}

func WrapVM(vm innerVM) *VM {
	return &VM{innerVM: vm}
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
	return vm.innerVM.Initialize(
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
