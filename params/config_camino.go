// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Gas Price
const (
	SunrisePhase0ExtraDataSize        = 0
	SunrisePhase0BaseFee       uint64 = 50_000_000_000
)

var (
	// CaminoChainConfig is the configuration for Camino Main Network
	CaminoChainConfig = &ChainConfig{
		ChainID:                         CaminoChainID,
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    big.NewInt(0),
		DAOForkSupport:                  true,
		EIP150Block:                     big.NewInt(0),
		EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
		ApricotPhase1BlockTimestamp:     big.NewInt(time.Date(2021, time.March, 31, 14, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase2BlockTimestamp:     big.NewInt(time.Date(2021, time.May, 10, 11, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase3BlockTimestamp:     big.NewInt(time.Date(2021, time.August, 24, 14, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase4BlockTimestamp:     big.NewInt(time.Date(2021, time.September, 22, 21, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase5BlockTimestamp:     big.NewInt(time.Date(2021, time.December, 2, 18, 0, 0, 0, time.UTC).Unix()),
		SunrisePhase0BlockTimestamp:     big.NewInt(time.Date(2022, time.May, 1, 15, 0, 0, 0, time.UTC).Unix()),
		ApricotPhasePre6BlockTimestamp:  big.NewInt(time.Date(2022, time.September, 5, 1, 30, 0, 0, time.UTC).Unix()),
		ApricotPhase6BlockTimestamp:     big.NewInt(time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC).Unix()),
		ApricotPhasePost6BlockTimestamp: big.NewInt(time.Date(2022, time.September, 7, 3, 0, 0, 0, time.UTC).Unix()),
		BanffBlockTimestamp:             big.NewInt(time.Date(2022, time.October, 18, 16, 0, 0, 0, time.UTC).Unix()),
		// TODO Add Cortina timestamps
	}

	// ColumbusChainConfig is the configuration for Columbus Test Network
	ColumbusChainConfig = &ChainConfig{
		ChainID:                         ColumbusChainID,
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    big.NewInt(0),
		DAOForkSupport:                  true,
		EIP150Block:                     big.NewInt(0),
		EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
		ApricotPhase1BlockTimestamp:     big.NewInt(time.Date(2021, time.March, 31, 14, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase2BlockTimestamp:     big.NewInt(time.Date(2021, time.May, 10, 11, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase3BlockTimestamp:     big.NewInt(time.Date(2021, time.August, 24, 14, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase4BlockTimestamp:     big.NewInt(time.Date(2021, time.September, 22, 21, 0, 0, 0, time.UTC).Unix()),
		ApricotPhase5BlockTimestamp:     big.NewInt(time.Date(2021, time.December, 2, 18, 0, 0, 0, time.UTC).Unix()),
		SunrisePhase0BlockTimestamp:     big.NewInt(time.Date(2022, time.May, 1, 15, 0, 0, 0, time.UTC).Unix()),
		ApricotPhasePre6BlockTimestamp:  big.NewInt(time.Date(2022, time.September, 5, 1, 30, 0, 0, time.UTC).Unix()),
		ApricotPhase6BlockTimestamp:     big.NewInt(time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC).Unix()),
		ApricotPhasePost6BlockTimestamp: big.NewInt(time.Date(2022, time.September, 7, 3, 0, 0, 0, time.UTC).Unix()),
		BanffBlockTimestamp:             big.NewInt(time.Date(2022, time.October, 18, 16, 0, 0, 0, time.UTC).Unix()),
		// TODO Add Cortina timestamps
	}
)

// CaminoRules returns the Camino modified rules to support Camino
// network upgrades
func (c *ChainConfig) CaminoRules(blockNum, blockTimestamp *big.Int) Rules {
	rules := c.AvalancheRules(blockNum, blockTimestamp)

	rules.IsSunrisePhase0 = c.IsSunrisePhase0(blockTimestamp)
	return rules
}
