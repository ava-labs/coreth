// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type ValidatorSet struct {
	set set.Set[ids.NodeID]
}

func (v *ValidatorSet) Has(ctx context.Context, nodeID ids.NodeID) bool {
	return v.set.Contains(nodeID)
}
