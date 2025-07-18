// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/libevm/core/types"
)

func calculatePredicateResults(
	ctx context.Context,
	snowContext *snow.Context,
	rules params.Rules,
	blockContext *block.Context,
	txs []*types.Transaction,
) (*predicate.Results, error) {
	predicateContext := precompileconfig.PredicateContext{
		SnowCtx:            snowContext,
		ProposerVMBlockCtx: blockContext,
	}
	predicateResults := predicate.NewResults()
	// TODO: Each transaction's predicates should be able to be calculated
	// concurrently.
	for _, tx := range txs {
		results, err := core.CheckPredicates(rules, &predicateContext, tx)
		if err != nil {
			return nil, err
		}
		predicateResults.SetTxResults(tx.Hash(), results)
	}
	return predicateResults, nil
}
