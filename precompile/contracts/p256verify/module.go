// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p256verify

import (
	"fmt"

	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"

	"github.com/ava-labs/libevm/common"
)

var _ contract.Configurator = (*configurator)(nil)

// ConfigKey is the key used in json config files to specify this precompile config.
// must be unique across all precompiles.
const ConfigKey = "p256verifyConfig"

// ContractAddress is the address of the p256verify precompile contract
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000000100")

// Module is the precompile module. It is used to register the precompile contract.
var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     &P256VerifyPrecompile{},
	Configurator: &configurator{},
}

type configurator struct{}

func init() {
	// Register the precompile module.
	// Each precompile contract registers itself through [RegisterModule] function.
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

// MakeConfig returns a new precompile config instance.
// This is required to Marshal/Unmarshal the precompile config.
func (*configurator) MakeConfig() precompileconfig.Config {
	return new(Config)
}

// Configure is a no-op for p256verify since it does not need to store any information in the state
func (*configurator) Configure(chainConfig precompileconfig.ChainConfig, cfg precompileconfig.Config, state contract.StateDB, _ contract.ConfigurationBlockContext) error {
	if _, ok := cfg.(*Config); !ok {
		return fmt.Errorf("expected config type %T, got %T: %v", &Config{}, cfg, cfg)
	}
	return nil
}
