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
// Copyright 2022 The go-ethereum Authors
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
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package pathdb

import (
	cmetrics "github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/libevm/metrics"
)

// nolint: unused
var (
	cleanHitMeter   = metrics.NewRegisteredMeter("pathdb/clean/hit", cmetrics.Registry)
	cleanMissMeter  = metrics.NewRegisteredMeter("pathdb/clean/miss", cmetrics.Registry)
	cleanReadMeter  = metrics.NewRegisteredMeter("pathdb/clean/read", cmetrics.Registry)
	cleanWriteMeter = metrics.NewRegisteredMeter("pathdb/clean/write", cmetrics.Registry)

	dirtyHitMeter         = metrics.NewRegisteredMeter("pathdb/dirty/hit", cmetrics.Registry)
	dirtyMissMeter        = metrics.NewRegisteredMeter("pathdb/dirty/miss", cmetrics.Registry)
	dirtyReadMeter        = metrics.NewRegisteredMeter("pathdb/dirty/read", cmetrics.Registry)
	dirtyWriteMeter       = metrics.NewRegisteredMeter("pathdb/dirty/write", cmetrics.Registry)
	dirtyNodeHitDepthHist = metrics.NewRegisteredHistogram("pathdb/dirty/depth", cmetrics.Registry, metrics.NewExpDecaySample(1028, 0.015))

	cleanFalseMeter = metrics.NewRegisteredMeter("pathdb/clean/false", cmetrics.Registry)
	dirtyFalseMeter = metrics.NewRegisteredMeter("pathdb/dirty/false", cmetrics.Registry)
	diskFalseMeter  = metrics.NewRegisteredMeter("pathdb/disk/false", cmetrics.Registry)

	commitTimeTimer  = metrics.NewRegisteredTimer("pathdb/commit/time", cmetrics.Registry)
	commitNodesMeter = metrics.NewRegisteredMeter("pathdb/commit/nodes", cmetrics.Registry)
	commitBytesMeter = metrics.NewRegisteredMeter("pathdb/commit/bytes", cmetrics.Registry)

	gcNodesMeter = metrics.NewRegisteredMeter("pathdb/gc/nodes", cmetrics.Registry)
	gcBytesMeter = metrics.NewRegisteredMeter("pathdb/gc/bytes", cmetrics.Registry)

	diffLayerBytesMeter = metrics.NewRegisteredMeter("pathdb/diff/bytes", cmetrics.Registry)
	diffLayerNodesMeter = metrics.NewRegisteredMeter("pathdb/diff/nodes", cmetrics.Registry)

	historyBuildTimeMeter  = metrics.NewRegisteredTimer("pathdb/history/time", cmetrics.Registry)
	historyDataBytesMeter  = metrics.NewRegisteredMeter("pathdb/history/bytes/data", cmetrics.Registry)
	historyIndexBytesMeter = metrics.NewRegisteredMeter("pathdb/history/bytes/index", cmetrics.Registry)
)
