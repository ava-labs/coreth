// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	sae "github.com/ava-labs/strevm"
	"github.com/ava-labs/strevm/adaptor"
)

func main() {
	vm := adaptor.Convert(&sae.SinceGenesis{
		Hooks: &hooks{},
	})

	rpcchainvm.Serve(context.Background(), vm)
}
