// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p256verify

import (
	"errors"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
)

var (
	errP256VerifyCannotBeActivated = errors.New("p256verify cannot be activated before Granite")
)

// Config implements the StatefulPrecompileConfig interface while adding in the
// p256verify specific configuration.
type Config struct {
	precompileconfig.Upgrade
}

// Key returns the unique key for the p256verify precompile.
func (c *Config) Key() string {
	return ConfigKey
}

// Timestamp returns the timestamp at which this stateful precompile should be enabled.
// This precompile is enabled at the Granite upgrade.
func (c *Config) Timestamp() *uint64 {
	return params.GetExtra(params.TestGraniteChainConfig).GraniteTimestamp
}

// IsDisabled returns true if this network upgrade should disable the precompile.
func (c *Config) IsDisabled() bool {
	return false
}

// Equal returns true if the provided argument configures the same precompile with the same parameters.
func (c *Config) Equal(cfg precompileconfig.Config) bool {
	_, ok := cfg.(*Config)
	return ok
}

// Verify is called on startup and an error is treated as fatal. Configure can assume the Config has passed verification.
func (c *Config) Verify(chainConfig precompileconfig.ChainConfig) error {
	// If p256verify attempts to activate before Granite, fail verification
	if c.Timestamp() != nil {
		timestamp := *c.Timestamp()
		if !chainConfig.IsGranite(timestamp) {
			return errP256VerifyCannotBeActivated
		}
	}
	return nil
}
