// Copyright (C) 2022, Chain4Travel AG. All rights reserved.

package admin

import (
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

type StateDB interface {
	GetState(common.Address, common.Hash) common.Hash
}

type EmptyStruct struct{}

// Admin interface to control administrative tasks, which are intended to
// be controlled on block level like BaseFee or Blacklisting or KYC
type AdminController interface {
	// Get the FixedBaseFee which should applied for blocks after height
	GetFixedBaseFee(head *types.Header, state StateDB) *big.Int
	// Returns true if we are in SunrisePhase0 and KYC flag is set
	KycVerified(head *types.Header, state StateDB, addr common.Address) bool
}
