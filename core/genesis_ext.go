// (c) 2025 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import "github.com/ava-labs/libevm/common"

type genesisBlockOptions struct {
	lastAcceptedHash               common.Hash
	skipChainConfigCheckCompatible bool
}

func makeGenesisBlockOptions(options []genesisBlockOption) genesisBlockOptions {
	var opts genesisBlockOptions
	for _, option := range options {
		option(&opts)
	}
	return opts
}

type genesisBlockOption func(*genesisBlockOptions)

func withLastAcceptedHash(hash common.Hash) genesisBlockOption {
	return func(o *genesisBlockOptions) {
		o.lastAcceptedHash = hash
	}
}

func withSkipChainConfigCheckCompatible(value bool) genesisBlockOption {
	return func(o *genesisBlockOptions) {
		o.skipChainConfigCheckCompatible = value
	}
}
