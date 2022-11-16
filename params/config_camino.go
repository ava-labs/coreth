// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

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
		HomesteadBlock:                  common.Big0,
		DAOForkBlock:                    common.Big0,
		DAOForkSupport:                  true,
		EIP150Block:                     common.Big0,
		EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                     common.Big0,
		EIP158Block:                     common.Big0,
		ByzantiumBlock:                  common.Big0,
		ConstantinopleBlock:             common.Big0,
		PetersburgBlock:                 common.Big0,
		IstanbulBlock:                   common.Big0,
		MuirGlacierBlock:                common.Big0,
		ApricotPhase1BlockTimestamp:     common.Big0,
		ApricotPhase2BlockTimestamp:     common.Big0,
		ApricotPhase3BlockTimestamp:     common.Big0,
		ApricotPhase4BlockTimestamp:     common.Big0,
		ApricotPhase5BlockTimestamp:     common.Big0,
		SunrisePhase0BlockTimestamp:     common.Big0,
		ApricotPhasePre6BlockTimestamp:  common.Big0,
		ApricotPhase6BlockTimestamp:     common.Big0,
		ApricotPhasePost6BlockTimestamp: common.Big0,
		BanffBlockTimestamp:             common.Big0,
		// TODO Add Cortina timestamps
	}

	// ColumbusChainConfig is the configuration for Columbus Test Network
	ColumbusChainConfig = &ChainConfig{
		ChainID:                         ColumbusChainID,
		HomesteadBlock:                  common.Big0,
		DAOForkBlock:                    common.Big0,
		DAOForkSupport:                  true,
		EIP150Block:                     common.Big0,
		EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                     common.Big0,
		EIP158Block:                     common.Big0,
		ByzantiumBlock:                  common.Big0,
		ConstantinopleBlock:             common.Big0,
		PetersburgBlock:                 common.Big0,
		IstanbulBlock:                   common.Big0,
		MuirGlacierBlock:                common.Big0,
		ApricotPhase1BlockTimestamp:     common.Big0,
		ApricotPhase2BlockTimestamp:     common.Big0,
		ApricotPhase3BlockTimestamp:     common.Big0,
		ApricotPhase4BlockTimestamp:     common.Big0,
		ApricotPhase5BlockTimestamp:     common.Big0,
		SunrisePhase0BlockTimestamp:     common.Big0,
		ApricotPhasePre6BlockTimestamp:  common.Big0,
		ApricotPhase6BlockTimestamp:     common.Big0,
		ApricotPhasePost6BlockTimestamp: common.Big0,
		BanffBlockTimestamp:             common.Big0,
		// TODO Add Cortina timestamps
	}

	// KopernikusChainConfig is the configuration for Kopernikus Dev Network
	KopernikusChainConfig = &ChainConfig{
		ChainID:                         KopernikusChainID,
		HomesteadBlock:                  common.Big0,
		DAOForkBlock:                    common.Big0,
		DAOForkSupport:                  true,
		EIP150Block:                     common.Big0,
		EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                     common.Big0,
		EIP158Block:                     common.Big0,
		ByzantiumBlock:                  common.Big0,
		ConstantinopleBlock:             common.Big0,
		PetersburgBlock:                 common.Big0,
		IstanbulBlock:                   common.Big0,
		MuirGlacierBlock:                common.Big0,
		ApricotPhase1BlockTimestamp:     common.Big0,
		ApricotPhase2BlockTimestamp:     common.Big0,
		ApricotPhase3BlockTimestamp:     common.Big0,
		ApricotPhase4BlockTimestamp:     common.Big0,
		ApricotPhase5BlockTimestamp:     common.Big0,
		SunrisePhase0BlockTimestamp:     common.Big0,
		ApricotPhasePre6BlockTimestamp:  common.Big0,
		ApricotPhase6BlockTimestamp:     common.Big0,
		ApricotPhasePost6BlockTimestamp: common.Big0,
		BanffBlockTimestamp:             common.Big0,
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
