package core

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ secp256k1fx.AliasGetter = AliasSet{}

type AliasSet map[ids.ShortID]*multisig.AliasWithNonce

func (as AliasSet) GetMultisigAlias(id ids.ShortID) (*multisig.AliasWithNonce, error) {
	alias, has := as[id]
	if !has {
		return nil, database.ErrNotFound
	}
	return alias, nil
}

func (as *AliasSet) Add(aliasInfs ...verify.State) {
	for _, ali := range aliasInfs {
		alias, ok := ali.(*multisig.AliasWithNonce)
		if !ok {
			continue
		}

		if old, has := (*as)[alias.ID]; has && old.Nonce > alias.Nonce {
			continue
		}
		(*as)[alias.ID] = alias
	}
}
