package evm

import (
	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ pubsub.Filterer = &filterer{}

type filterer struct {
	importTx *UnsignedImportTx
	exportTx *UnsignedExportTx
}

func NewPubSubImportFilterer(tx *UnsignedImportTx) pubsub.Filterer {
	return &filterer{importTx: tx}
}

func NewPubSubExportFilterer(tx *UnsignedExportTx) pubsub.Filterer {
	return &filterer{exportTx: tx}
}

// Apply the filter on the addresses.
func (f *filterer) Filter(filters []pubsub.Filter) ([]bool, interface{}) {
	resp := make([]bool, len(filters))
	var txID ids.ID

	if f.importTx != nil {
		txID = f.importTx.ID()
		for _, address := range f.importTx.Addresses() {
			for i, c := range filters {
				if resp[i] {
					continue
				}
				resp[i] = c.Check(address)
			}
		}
	} else if f.exportTx != nil {
		txID = f.exportTx.ID()
		for _, utxo := range f.exportTx.ExportedOutputs {
			addressable, ok := utxo.Out.(avax.Addressable)
			if !ok {
				continue
			}

			for _, address := range addressable.Addresses() {
				for i, c := range filters {
					if resp[i] {
						continue
					}
					resp[i] = c.Check(address)
				}
			}
		}
	}

	return resp, api.JSONTxID{
		TxID: txID,
	}
}
