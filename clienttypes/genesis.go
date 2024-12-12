package clienttypes

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/coreth/utils"
	ethcore "github.com/ava-labs/libevm/core"
	ethparams "github.com/ava-labs/libevm/params"
)

func MarshalGenesis(
	ethGenesis *ethcore.Genesis, chainConfigExtra *ChainConfigExtra,
) ([]byte, error) {
	if ethGenesis == nil || ethGenesis.Config == nil {
		return nil, errors.New("genesis or genesis.Config is nil")
	}
	bytes, err := json.Marshal(ethGenesis)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return nil, err
	}

	// ethGenesis.Config is non-nil so it's expected that raw["config"] is
	// map[string]interface{}
	rawConfig, ok := raw["config"].(map[string]interface{})
	if !ok {
		return nil, errors.New("genesis.Config is not map[string]interface{}")
	}

	// Marshal chainConfigExtra and unmarshal it into rawConfig
	extraBytes, err := json.Marshal(chainConfigExtra)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(extraBytes, &rawConfig); err != nil {
		return nil, err
	}

	return json.Marshal(raw)
}

func GetChainConfig(agoUpgrade upgrade.Config, chainID *big.Int) (*ethparams.ChainConfig, *ChainConfigExtra) {
	return &ethparams.ChainConfig{
			ChainID:             chainID,
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
		}, &ChainConfigExtra{
			NetworkUpgrades: getNetworkUpgrades(agoUpgrade),
		}
}

func getNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		ApricotPhase1BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase1Time),
		ApricotPhase2BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase2Time),
		ApricotPhase3BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase3Time),
		ApricotPhase4BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase4Time),
		ApricotPhase5BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase5Time),
		ApricotPhasePre6BlockTimestamp:  utils.TimeToNewUint64(agoUpgrade.ApricotPhasePre6Time),
		ApricotPhase6BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase6Time),
		ApricotPhasePost6BlockTimestamp: utils.TimeToNewUint64(agoUpgrade.ApricotPhasePost6Time),
		BanffBlockTimestamp:             utils.TimeToNewUint64(agoUpgrade.BanffTime),
		CortinaBlockTimestamp:           utils.TimeToNewUint64(agoUpgrade.CortinaTime),
		DurangoBlockTimestamp:           utils.TimeToNewUint64(agoUpgrade.DurangoTime),
		EtnaTimestamp:                   utils.TimeToNewUint64(agoUpgrade.EtnaTime),
	}
}
