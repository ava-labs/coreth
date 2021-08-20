package evm

import (
	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/pubsub"
)

var _ pubsub.Filterer = &filterer{}

type filterer struct {
	tx *UnsignedImportTx
}

func NewPubSubFilterer(tx *UnsignedImportTx) pubsub.Filterer {
	return &filterer{tx: tx}
}

// Apply the filter on the addresses.
func (f *filterer) Filter(filters []pubsub.Filter) ([]bool, interface{}) {
	resp := make([]bool, len(filters))
	for _, address := range f.tx.Addresses() {
		for i, c := range filters {
			if resp[i] {
				continue
			}
			resp[i] = c.Check(address)
		}
	}

	return resp, api.JSONTxID{
		TxID: f.tx.ID(),
	}
}
