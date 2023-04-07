package core

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/stretchr/testify/require"
)

func TestAliasSetGetMultisigAlias(t *testing.T) {
	tests := map[string]struct {
		set           *AliasSet
		aliases       []verify.State
		query         ids.ShortID
		expectedErr   error
		expectedNonce uint64
	}{
		"read from empty set": {
			set:         &AliasSet{},
			query:       ids.ShortEmpty,
			expectedErr: database.ErrNotFound,
		},
		"add to empty set": {
			set: &AliasSet{},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
				},
			},
			expectedErr: database.ErrNotFound,
		},
		"add & read from empty set": {
			set: &AliasSet{},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
				},
			},
			query: ids.ShortID{1},
		},
		"add newer expected newer": {
			set: &AliasSet{},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 1,
				},
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 0,
				},
			},
			query:         ids.ShortID{1},
			expectedNonce: 1,
		},
		"add older expected newer": {
			set: &AliasSet{},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 0,
				},
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 1,
				},
			},
			query:         ids.ShortID{1},
			expectedNonce: 1,
		},
		"add many expected latest": {
			set: &AliasSet{},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 2,
				},
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 0,
				},
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 1,
				},
			},
			query:         ids.ShortID{1},
			expectedNonce: 2,
		},
		"set contains older expect newer": {
			set: &AliasSet{
				ids.ShortID{1}: {
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 0,
				},
			},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 2,
				},
			},
			query:         ids.ShortID{1},
			expectedNonce: 2,
		},
		"set contains newer expect newer": {
			set: &AliasSet{
				ids.ShortID{1}: {
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 2,
				},
			},
			aliases: []verify.State{
				&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID: ids.ShortID{1},
					},
					Nonce: 1,
				},
			},
			query:         ids.ShortID{1},
			expectedNonce: 2,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			testSet := test.set
			testSet.Add(test.aliases...)

			alias, err := test.set.GetMultisigAlias(test.query)
			require.ErrorIs(t, err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}

			require.Equal(t, test.query, alias.ID)
			require.Equal(t, test.expectedNonce, alias.Nonce)
		})
	}
}
