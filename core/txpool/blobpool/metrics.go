// (c) 2024, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2023 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package blobpool

import (
	cmetrics "github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/libevm/metrics"
)

var (
	// datacapGauge tracks the user's configured capacity for the blob pool. It
	// is mostly a way to expose/debug issues.
	datacapGauge = metrics.NewRegisteredGauge("blobpool/datacap", cmetrics.Registry)

	// The below metrics track the per-datastore metrics for the primary blob
	// store and the temporary limbo store.
	datausedGauge = metrics.NewRegisteredGauge("blobpool/dataused", cmetrics.Registry)
	datarealGauge = metrics.NewRegisteredGauge("blobpool/datareal", cmetrics.Registry)
	slotusedGauge = metrics.NewRegisteredGauge("blobpool/slotused", cmetrics.Registry)

	limboDatausedGauge = metrics.NewRegisteredGauge("blobpool/limbo/dataused", cmetrics.Registry)
	limboDatarealGauge = metrics.NewRegisteredGauge("blobpool/limbo/datareal", cmetrics.Registry)
	limboSlotusedGauge = metrics.NewRegisteredGauge("blobpool/limbo/slotused", cmetrics.Registry)

	// The below metrics track the per-shelf metrics for the primary blob store
	// and the temporary limbo store.
	shelfDatausedGaugeName = "blobpool/shelf_%d/dataused"
	shelfDatagapsGaugeName = "blobpool/shelf_%d/datagaps"
	shelfSlotusedGaugeName = "blobpool/shelf_%d/slotused"
	shelfSlotgapsGaugeName = "blobpool/shelf_%d/slotgaps"

	limboShelfDatausedGaugeName = "blobpool/limbo/shelf_%d/dataused"
	limboShelfDatagapsGaugeName = "blobpool/limbo/shelf_%d/datagaps"
	limboShelfSlotusedGaugeName = "blobpool/limbo/shelf_%d/slotused"
	limboShelfSlotgapsGaugeName = "blobpool/limbo/shelf_%d/slotgaps"

	// The oversized metrics aggregate the shelf stats above the max blob count
	// limits to track transactions that are just huge, but don't contain blobs.
	//
	// There are no oversized data in the limbo, it only contains blobs and some
	// constant metadata.
	oversizedDatausedGauge = metrics.NewRegisteredGauge("blobpool/oversized/dataused", cmetrics.Registry)
	oversizedDatagapsGauge = metrics.NewRegisteredGauge("blobpool/oversized/datagaps", cmetrics.Registry)
	oversizedSlotusedGauge = metrics.NewRegisteredGauge("blobpool/oversized/slotused", cmetrics.Registry)
	oversizedSlotgapsGauge = metrics.NewRegisteredGauge("blobpool/oversized/slotgaps", cmetrics.Registry)

	// basefeeGauge and blobfeeGauge track the current network 1559 base fee and
	// 4844 blob fee respectively.
	basefeeGauge = metrics.NewRegisteredGauge("blobpool/basefee", cmetrics.Registry)
	blobfeeGauge = metrics.NewRegisteredGauge("blobpool/blobfee", cmetrics.Registry)

	// pooltipGauge is the configurable miner tip to permit a transaction into
	// the pool.
	pooltipGauge = metrics.NewRegisteredGauge("blobpool/pooltip", cmetrics.Registry)

	// addwait/time, resetwait/time and getwait/time track the rough health of
	// the pool and whether it's capable of keeping up with the load from the
	// network.
	addwaitHist   = metrics.NewRegisteredHistogram("blobpool/addwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	addtimeHist   = metrics.NewRegisteredHistogram("blobpool/addtime", nil, metrics.NewExpDecaySample(1028, 0.015))
	getwaitHist   = metrics.NewRegisteredHistogram("blobpool/getwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	gettimeHist   = metrics.NewRegisteredHistogram("blobpool/gettime", nil, metrics.NewExpDecaySample(1028, 0.015))
	pendwaitHist  = metrics.NewRegisteredHistogram("blobpool/pendwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	pendtimeHist  = metrics.NewRegisteredHistogram("blobpool/pendtime", nil, metrics.NewExpDecaySample(1028, 0.015))
	resetwaitHist = metrics.NewRegisteredHistogram("blobpool/resetwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	resettimeHist = metrics.NewRegisteredHistogram("blobpool/resettime", nil, metrics.NewExpDecaySample(1028, 0.015))

	// The below metrics track various cases where transactions are dropped out
	// of the pool. Most are exceptional, some are chain progression and some
	// threshold cappings.
	dropInvalidMeter     = metrics.NewRegisteredMeter("blobpool/drop/invalid", cmetrics.Registry)     // Invalid transaction, consensus change or bugfix, neutral-ish
	dropDanglingMeter    = metrics.NewRegisteredMeter("blobpool/drop/dangling", cmetrics.Registry)    // First nonce gapped, bad
	dropFilledMeter      = metrics.NewRegisteredMeter("blobpool/drop/filled", cmetrics.Registry)      // State full-overlap, chain progress, ok
	dropOverlappedMeter  = metrics.NewRegisteredMeter("blobpool/drop/overlapped", cmetrics.Registry)  // State partial-overlap, chain progress, ok
	dropRepeatedMeter    = metrics.NewRegisteredMeter("blobpool/drop/repeated", cmetrics.Registry)    // Repeated nonce, bad
	dropGappedMeter      = metrics.NewRegisteredMeter("blobpool/drop/gapped", cmetrics.Registry)      // Non-first nonce gapped, bad
	dropOverdraftedMeter = metrics.NewRegisteredMeter("blobpool/drop/overdrafted", cmetrics.Registry) // Balance exceeded, bad
	dropOvercappedMeter  = metrics.NewRegisteredMeter("blobpool/drop/overcapped", cmetrics.Registry)  // Per-account cap exceeded, bad
	dropOverflownMeter   = metrics.NewRegisteredMeter("blobpool/drop/overflown", cmetrics.Registry)   // Global disk cap exceeded, neutral-ish
	dropUnderpricedMeter = metrics.NewRegisteredMeter("blobpool/drop/underpriced", cmetrics.Registry) // Gas tip changed, neutral
	dropReplacedMeter    = metrics.NewRegisteredMeter("blobpool/drop/replaced", cmetrics.Registry)    // Transaction replaced, neutral

	// The below metrics track various outcomes of transactions being added to
	// the pool.
	addInvalidMeter      = metrics.NewRegisteredMeter("blobpool/add/invalid", cmetrics.Registry)      // Invalid transaction, reject, neutral
	addUnderpricedMeter  = metrics.NewRegisteredMeter("blobpool/add/underpriced", cmetrics.Registry)  // Gas tip too low, neutral
	addStaleMeter        = metrics.NewRegisteredMeter("blobpool/add/stale", cmetrics.Registry)        // Nonce already filled, reject, bad-ish
	addGappedMeter       = metrics.NewRegisteredMeter("blobpool/add/gapped", cmetrics.Registry)       // Nonce gapped, reject, bad-ish
	addOverdraftedMeter  = metrics.NewRegisteredMeter("blobpool/add/overdrafted", cmetrics.Registry)  // Balance exceeded, reject, neutral
	addOvercappedMeter   = metrics.NewRegisteredMeter("blobpool/add/overcapped", cmetrics.Registry)   // Per-account cap exceeded, reject, neutral
	addNoreplaceMeter    = metrics.NewRegisteredMeter("blobpool/add/noreplace", cmetrics.Registry)    // Replacement fees or tips too low, neutral
	addNonExclusiveMeter = metrics.NewRegisteredMeter("blobpool/add/nonexclusive", cmetrics.Registry) // Plain transaction from same account exists, reject, neutral
	addValidMeter        = metrics.NewRegisteredMeter("blobpool/add/valid", cmetrics.Registry)        // Valid transaction, add, neutral
)
