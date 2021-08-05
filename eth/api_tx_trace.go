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

package eth

import (
	"context"
	"fmt"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/core/vm/tracelogger"
	"github.com/ava-labs/coreth/rpc"

	"github.com/ethereum/go-ethereum/common"
)

// PublicTxTraceAPI provides an API to tracing transaction or block information.
// It offers only methods that operate on public data that is freely available to anyone.
type PublicTxTraceAPI struct {
	e *Ethereum
}

// NewPublicTxTraceAPI creates a new trace API.
func NewPublicTxTraceAPI(e *Ethereum) *PublicTxTraceAPI {
	return &PublicTxTraceAPI{e: e}
}

// txTraceContext is the contextual infos about a transaction before it gets run.
type txTraceContext struct {
	tx    *types.Transaction
	index int         // Index of the transaction within the block
	block common.Hash // Hash of the block containing the transaction
}

// Transaction trace_transaction function returns transaction traces.
func (api *PublicTxTraceAPI) Transaction(ctx context.Context, txHash common.Hash) (interface{}, error) {
	if api.e.blockchain == nil {
		return []byte{}, fmt.Errorf("blockchain corruput")
	}

	tx, blockHash, blockNumber, index, err := api.e.APIBackend.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction %#v not found", txHash)
	}
	// It shouldn't happen in practice.
	if blockNumber == 0 {
		return nil, fmt.Errorf("genesis is not traceable")
	}

	block, err := api.e.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blockNumber))
	if err != nil {
		return nil, err
	}

	msg, vmctx, statedb, err := api.e.APIBackend.StateAtTransaction(ctx, block, int(index), 128)
	if err != nil {
		return nil, err
	}

	txctx := &txTraceContext{
		tx:    tx,
		index: int(index),
		block: blockHash,
	}

	return api.traceTx(ctx, msg, txctx, vmctx, statedb)
}

func (api *PublicTxTraceAPI) traceTx(ctx context.Context, message core.Message, txctx *txTraceContext, vmctx vm.BlockContext, statedb *state.StateDB) (interface{}, error) {
	var (
		tracer    *tracelogger.StructLogger
		err       error
		txContext = core.NewEVMTxContext(message)
	)

	// Construct trace logger to record result as parity's one
	tracer = tracelogger.NewTraceStructLogger(nil)
	vmenv := vm.NewEVM(vmctx, txContext, statedb, api.e.APIBackend.ChainConfig(), vm.Config{Debug: true, Tracer: tracer})
	statedb.Prepare(txctx.tx.Hash(), txctx.block, txctx.index)

	// Fill essential info into logger
	tracer.SetFrom(message.From())
	tracer.SetTo(message.To())
	tracer.SetValue(*message.Value())
	tracer.SetGasUsed(txctx.tx.Gas())
	tracer.SetBlockHash(txctx.block)
	tracer.SetBlockNumber(vmctx.BlockNumber)
	tracer.SetTx(txctx.tx.Hash())
	tracer.SetTxIndex(uint(txctx.index))

	_, err = core.ApplyMessage(vmenv, message, new(core.GasPool).AddGas(message.Gas()))
	if err != nil {
		return nil, fmt.Errorf("tracing failed: %v", err)
	}

	tracer.Finalize()
	return tracer.GetResult(), nil
}
