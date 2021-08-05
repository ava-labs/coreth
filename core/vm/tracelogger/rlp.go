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
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
)

type flatTrace struct {
	// Action fields
	ActionCallType      *string         `rlp:"nil"`
	ActionFrom          *common.Address `rlp:"nil"`
	ActionTo            *common.Address `rlp:"nil"`
	ActionValue         big.Int
	ActionGas           uint64
	ActionInit          []byte
	ActionInput         []byte
	ActionAddress       *common.Address `rlp:"nil"`
	ActionRefundAddress *common.Address `rlp:"nil"`
	ActionBalance       *big.Int        `rlp:"nil"`
	// Result fields
	ResultGasUsed uint64
	ResultOutput  []byte
	ResultCode    []byte
	ResultAddress *common.Address `rlp:"nil"`
	// Other fields
	Error        string
	Subtraces    uint64
	TraceAddress []uint32
	TraceType    string
}

type ActionTraces []ActionTrace

func (traces *ActionTraces) EncodeRLP(w io.Writer) error {
	cpy := make([][]byte, 0, len(*traces))
	for _, t := range *traces {
		bs, err := rlp.EncodeToBytes(&t)
		if err != nil {
			return err
		}
		cpy = append(cpy, bs)
	}
	return rlp.Encode(w, &cpy)
}

func (traces *ActionTraces) DecodeRLP(s *rlp.Stream) error {
	raw := make([][]byte, 0)
	if err := s.Decode(&raw); err != nil {
		return err
	}
	cpy := make([]ActionTrace, 0, len(raw))
	for _, bs := range raw {
		at := new(ActionTrace)
		err := rlp.DecodeBytes(bs, at)
		if err != nil {
			return err
		}
		cpy = append(cpy, *at)
	}
	*traces = cpy
	return nil
}

// EncodeRLP serializes ActionTrace into the Ethereum RLP flatTrace format.
func (at *ActionTrace) EncodeRLP(w io.Writer) error {
	ft := &flatTrace{
		ActionCallType:      at.Action.CallType,
		ActionFrom:          at.Action.From,
		ActionTo:            at.Action.To,
		ActionValue:         *at.Action.Value.ToInt(),
		ActionGas:           uint64(at.Action.Gas),
		ActionInit:          at.Action.Init,
		ActionInput:         at.Action.Input,
		ActionAddress:       at.Action.Address,
		ActionRefundAddress: at.Action.RefundAddress,
		ActionBalance:       at.Action.Balance.ToInt(),
		Error:               at.Error,
		Subtraces:           at.Subtraces,
		TraceAddress:        at.TraceAddress,
		TraceType:           at.TraceType,
	}
	if at.Result != nil {
		ft.ResultGasUsed = uint64(at.Result.GasUsed)
		if at.Result.Output != nil {
			ft.ResultOutput = *at.Result.Output
		}
		ft.ResultCode = at.Result.Code
		ft.ResultAddress = at.Result.Address
	}
	return rlp.Encode(w, ft)
}

// DecodeRLP Decodes the Ethereum RLP flatTrace.
func (at *ActionTrace) DecodeRLP(s *rlp.Stream) error {
	var ft flatTrace
	if err := s.Decode(&ft); err != nil {
		return err
	}

	action := AddressAction{
		CallType:      ft.ActionCallType,
		From:          ft.ActionFrom,
		To:            ft.ActionTo,
		Value:         hexutil.Big(ft.ActionValue),
		Gas:           hexutil.Uint64(ft.ActionGas),
		Init:          ft.ActionInit,
		Input:         ft.ActionInput,
		Address:       ft.ActionAddress,
		RefundAddress: ft.ActionRefundAddress,
		Balance:       (*hexutil.Big)(ft.ActionBalance),
	}
	result := &TraceActionResult{
		GasUsed: hexutil.Uint64(ft.ResultGasUsed),
		Code:    ft.ResultCode,
		Address: ft.ResultAddress,
	}
	if ft.ResultOutput != nil {
		output := hexutil.Bytes(ft.ResultOutput)
		result.Output = &output
	}

	// Set unrelated filed to nil explicitly for json decode omit.
	switch ft.TraceType {
	case CALL:
		action.Balance = nil
		result.Code = nil
	case CREATE:
		action.Balance = nil
		result.Output = nil
	case SELFDESTRUCT:
		result = nil
	default:
	}

	at.Action, at.Error, at.Subtraces, at.TraceAddress, at.TraceType = action, ft.Error, ft.Subtraces, ft.TraceAddress, ft.TraceType
	if at.Error == "" { // only succeeded trace has result filed
		at.Result = result
	}
	return nil
}
