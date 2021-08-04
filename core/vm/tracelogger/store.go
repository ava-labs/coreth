// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
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

package tracelogger

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// Store contains all the methods for tx-trace to interact with the underlying database.
type Store interface {
	// ReadTxTrace retrieve tracing result from underlying database.
	ReadTxTrace(ctx context.Context, txHash common.Hash) ([]byte, error)
	// WriteTxTrace write tracing result to underlying database.
	WriteTxTrace(ctx context.Context, txHash common.Hash, trace []byte) error
}
