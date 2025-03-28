package manipulation

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/counter"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var (
	globalManipulator *Manipulator
	initOnce          sync.Once
)

func InitGlobalConfig(configJSON string, logger log.Logger, txCounter *counter.TxCounter, chainConfig *params.ChainConfig) error {
	var err error
	initOnce.Do(func() {
		if configJSON == "" {
			globalManipulator = New(false, false, logger, txCounter, chainConfig)
			logger.Info("Manipulation disabled", "reason", "empty config")
			return
		}

		var config Config
		if err = json.Unmarshal([]byte(configJSON), &config); err != nil {
			err = fmt.Errorf("failed to parse config: %w", err)
			logger.Error("Config parsing failed", "error", err)
			return
		}

		globalManipulator = New(config.Enabled, config.DetectInjection, logger, txCounter, chainConfig)

		for _, addrStr := range config.CensoredAddresses {
			addr := common.HexToAddress(addrStr)
			globalManipulator.AddCensoredAddress(addr)
		}
		for _, addrStr := range config.PriorityAddresses {
			addr := common.HexToAddress(addrStr)
			globalManipulator.AddPriorityAddress(addr)
		}

		logger.Info("Manipulation initialized",
			"enabled", config.Enabled,
			"censored", len(config.CensoredAddresses),
			"priority", len(config.PriorityAddresses),
			"detect_injection", config.DetectInjection)
	})
	return err
}

func GetGlobalManipulator() *Manipulator {
	if globalManipulator == nil {
		globalManipulator = New(false, false, log.Root(), nil, nil)
	}
	return globalManipulator
}
