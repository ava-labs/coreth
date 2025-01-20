package vm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/coreth/plugin/evm/atomic/sync"
	"github.com/ava-labs/coreth/plugin/evm/message"

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

type ExtensibleEVM interface {
	SetNetworkCodec(codec codec.Manager) error
}

type InnerVM interface {
	ExtensibleEVM
	avalanchecommon.VM
	secp256k1fx.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

type VM struct {
	InnerVM // Inner EVM
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
	innerVM := vm.InnerVM
	// Register the codec for the atomic block sync summary
	networkCodec, err := message.NewCodec(sync.AtomicSyncSummary{})
	if err != nil {
		return fmt.Errorf("failed to create codec manager: %w", err)
	}
	if err := innerVM.SetNetworkCodec(networkCodec); err != nil {
		return fmt.Errorf("failed to set network codec: %w", err)
	}
	return innerVM.Initialize(
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
