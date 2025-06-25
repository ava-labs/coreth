// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

type StateAccountExtraType bool

func (sa StateAccountExtraType) IsMultiCoin() bool {
	return bool(sa)
}
