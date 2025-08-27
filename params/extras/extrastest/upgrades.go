// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"

	"github.com/ava-labs/coreth/params/extras"
)

func GetAvalancheRulesFromFork(fork upgradetest.Fork) extras.AvalancheRules {
	upgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(fork))
	return upgrades.GetAvalancheRules(uint64(upgrade.InitiallyActiveTime.Unix()))
}
