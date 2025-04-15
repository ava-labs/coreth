// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package miner

import (
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
)

const (
	// Leaves 256 KBs for other sections of the block (limit is 2MB).
	// This should suffice for atomic txs, proposervm header, and serialization overhead.
	targetTxsSize = 1792 * units.KiB
)

// withConcurrentWorkers is a simple wrapper to reduce diffs in the [worker.makeEnv] method
// compared to the Geth codebase, especially to have the state argument named `state`.
func withConcurrentWorkers(numPrefetchers int) state.PrefetcherOption {
	return state.WithConcurrentWorkers(numPrefetchers)
}

// isDurango is a small helper function with its main purpose being to reduce diffs
// in the [worker.generateWork] method compared to the Geth codebase. Notably, it allows
// to keep the argument name as `params` and not conflict with the `params` package.
func (w *worker) isDurango(work *environment) bool {
	rules := w.chainConfig.Rules(work.header.Number, params.IsMergeTODO, work.header.Time)
	rulesExtra := params.GetRulesExtra(rules)
	return rulesExtra.IsDurango
}
