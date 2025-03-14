// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import ethtypes "github.com/ava-labs/libevm/core/types"

// TODO(arr4n): this is a temporary workaround because of the circular
// dependency between the coreth and avalanchego repos. The latter depends on
// these types via coreth instead of via libevm so there's a chicken-and-egg
// problem with refactoring both repos.

type (
	// Receipt is an alias for use by avalanchego.
	//
	// Deprecated: use the RHS type directly.
	Receipt = ethtypes.Receipt

	// Transaction is an alias for use by avalanchego.
	// Deprecated: use the RHS type directly.
	Transaction = ethtypes.Transaction
)
