// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import "github.com/ethereum/go-ethereum/params"

func (r Rules) AsGeth(isMerge bool) params.Rules {
	return params.Rules{
		ChainID:          r.ChainID,
		IsHomestead:      r.IsHomestead,
		IsEIP150:         r.IsEIP150,
		IsEIP155:         r.IsEIP155,
		IsEIP158:         r.IsEIP158,
		IsByzantium:      r.IsByzantium,
		IsConstantinople: r.IsConstantinople,
		IsPetersburg:     r.IsPetersburg,
		IsIstanbul:       r.IsIstanbul,
		IsBerlin:         r.IsApricotPhase2,
		IsLondon:         r.IsApricotPhase3,
		IsMerge:          r.IsDUpgrade,
		IsShanghai:       r.IsDUpgrade,
		IsCancun:         r.IsCancun,
	}
}
