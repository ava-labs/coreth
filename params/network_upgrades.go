// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

func (c *ChainConfig) forkOrder() []fork {
	return []fork{
		{name: "apricotPhase1Time", time: c.ApricotPhase1Time},
		{name: "apricotPhase2Time", time: c.ApricotPhase2Time},
		{name: "apricotPhase3Time", time: c.ApricotPhase3Time},
		{name: "apricotPhase4Time", time: c.ApricotPhase4Time},
		{name: "apricotPhase5Time", time: c.ApricotPhase5Time},
		{name: "apricotPhasePre6Time", time: c.ApricotPhasePre6Time},
		{name: "apricotPhase6Time", time: c.ApricotPhase6Time},
		{name: "apricotPhasePost6Time", time: c.ApricotPhasePost6Time},
		{name: "banffTime", time: c.BanffTime},
		{name: "cortinaTime", time: c.CortinaTime},
		{name: "durangoTime", time: c.DurangoTime},
		{name: "eUpgradeTime", time: c.EUpgradeTime},
	}
}

type AvalancheRules struct {
	IsApricotPhase1, IsApricotPhase2, IsApricotPhase3, IsApricotPhase4, IsApricotPhase5 bool
	IsApricotPhasePre6, IsApricotPhase6, IsApricotPhasePost6                            bool
	IsBanff                                                                             bool
	IsCortina                                                                           bool
	IsDurango                                                                           bool
	IsEUpgrade                                                                          bool
}

func (c *ChainConfig) GetAvalancheRules(timestamp uint64) AvalancheRules {
	rules := AvalancheRules{}
	rules.IsApricotPhase1 = c.IsApricotPhase1(timestamp)
	rules.IsApricotPhase2 = c.IsApricotPhase2(timestamp)
	rules.IsApricotPhase3 = c.IsApricotPhase3(timestamp)
	rules.IsApricotPhase4 = c.IsApricotPhase4(timestamp)
	rules.IsApricotPhase5 = c.IsApricotPhase5(timestamp)
	rules.IsApricotPhasePre6 = c.IsApricotPhasePre6(timestamp)
	rules.IsApricotPhase6 = c.IsApricotPhase6(timestamp)
	rules.IsApricotPhasePost6 = c.IsApricotPhasePost6(timestamp)
	rules.IsBanff = c.IsBanff(timestamp)
	rules.IsCortina = c.IsCortina(timestamp)
	rules.IsDurango = c.IsDurango(timestamp)
	rules.IsEUpgrade = c.IsEUpgrade(timestamp)

	return rules
}
