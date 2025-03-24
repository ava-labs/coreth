// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	vmsync "github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
)

var (
	errVMAlreadyInitialized      = errors.New("vm already initialized")
	errExtensionConfigAlreadySet = errors.New("extension config already set")
)

func (vm *VM) SetExtensionConfig(config *extension.Config) error {
	if vm.ctx != nil {
		return errVMAlreadyInitialized
	}
	if vm.extensionConfig != nil {
		return errExtensionConfigAlreadySet
	}
	vm.extensionConfig = config
	return nil
}

// All these methods below assumes that VM is already initialized

func (vm *VM) GetVMBlock(ctx context.Context, blkID ids.ID) (extension.VMBlock, error) {
	// Since each internal handler used by [vm.State] always returns a block
	// with non-nil ethBlock value, GetBlockInternal should never return a
	// (*Block) with a nil ethBlock value.
	blk, err := vm.GetBlockInternal(ctx, blkID)
	if err != nil {
		return nil, err
	}

	return blk.(*Block), nil
}

func (vm *VM) LastAcceptedVMBlock() extension.VMBlock {
	lastAcceptedBlock := vm.LastAcceptedBlockInternal()
	if lastAcceptedBlock == nil {
		return nil
	}
	return lastAcceptedBlock.(*Block)
}

func (vm *VM) NewVMBlock(ethBlock *types.Block) (extension.VMBlock, error) {
	blk, err := vm.newBlock(ethBlock)
	if err != nil {
		return nil, err
	}

	return blk, nil
}

// IsBootstrapped returns true if the VM has finished bootstrapping
func (vm *VM) IsBootstrapped() bool {
	return vm.bootstrapped.Get()
}

func (vm *VM) Ethereum() *eth.Ethereum {
	return vm.eth
}

func (vm *VM) Config() config.Config {
	return vm.config
}

func (vm *VM) MetricRegistry() *prometheus.Registry {
	return vm.sdkMetrics
}

func (vm *VM) Validators() *p2p.Validators {
	return vm.p2pValidators
}

func (vm *VM) VersionDB() *versiondb.Database {
	return vm.versiondb
}

func (vm *VM) EthChainDB() ethdb.Database {
	return vm.chaindb
}

func (vm *VM) SyncerClient() vmsync.Client {
	return vm.Client
}

func (vm *VM) Version(context.Context) (string, error) {
	return Version, nil
}
